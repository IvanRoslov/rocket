package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// attachmentsTestDeps: messagesTestDeps plus an attachments dir under the
// test home.
func attachmentsTestDeps(t *testing.T) Deps {
	t.Helper()
	d := messagesTestDeps(t)
	d.Cfg.AttachmentsDir = filepath.Join(d.Cfg.Home, "attachments")
	return d
}

func postAttachment(t *testing.T, url, contentType string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/v1/attachments", contentType, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/attachments: %v", err)
	}
	return resp
}

func TestPostAttachment_HappyPath(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	payload := []byte("fake-png-bytes")
	resp := postAttachment(t, srv.URL, "image/png", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.URL != "/v1/attachments/"+itoa(got.ID) {
		t.Errorf("url = %q", got.URL)
	}

	// File landed on disk under <id>.png.
	onDisk, err := os.ReadFile(filepath.Join(d.Cfg.AttachmentsDir, itoa(got.ID)+".png"))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if !bytes.Equal(onDisk, payload) {
		t.Errorf("stored bytes differ")
	}

	// And GET serves it back with the right Content-Type.
	getResp := getJSON(t, srv.URL+"/v1/attachments/"+itoa(got.ID))
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}
	if ct := getResp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	served, _ := io.ReadAll(getResp.Body)
	if !bytes.Equal(served, payload) {
		t.Errorf("served bytes differ")
	}
}

func TestPostAttachment_UnsupportedMime(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postAttachment(t, srv.URL, "application/pdf", []byte("%PDF"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestPostAttachment_TooLarge(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postAttachment(t, srv.URL, "image/png", make([]byte, maxAttachmentBytes+1))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestGetAttachment_NotFound(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/attachments/999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestPostMessage_ResponseBodyIsRewritten covers the chat duplicate-bubble
// fix: handlePostMessage rewrites attachment links before storing (see
// rewriteAttachmentLinks), so the stored body differs from the body the web
// client sent. The 201 response must carry the STORED (rewritten) body, not
// the original request body, so the client can reconcile its optimistic
// bubble against the transcript echo.
func TestPostMessage_ResponseBodyIsRewritten(t *testing.T) {
	d := attachmentsTestDeps(t)
	srv := newTestServer(t, d)

	addMsgTestSession(t, d, "recv", "running")

	id, err := d.Store.AddAttachment(store.Attachment{MIME: "image/png", Size: 3})
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	sentBody := "see ![screenshot](/v1/attachments/" + itoa(id) + ")"
	resp := postJSON(t, srv.URL+"/v1/messages", map[string]any{
		"to":   "recv",
		"body": sentBody,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var got struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wantPath := filepath.Join(d.Cfg.AttachmentsDir, itoa(id)+".png")
	if !strings.Contains(got.Body, "[screenshot: "+wantPath+"]") {
		t.Errorf("response body not rewritten: %q", got.Body)
	}
	if got.Body == sentBody {
		t.Errorf("response body equals the original request body, want the rewritten form")
	}

	// The stored message must match what was echoed back, exactly — that's
	// what the transcript echo will show, and what the client's optimistic
	// dedupe compares against.
	stored, err := d.Store.GetMessage(got.ID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if stored.Body != got.Body {
		t.Errorf("stored body = %q, want equal to response body %q", stored.Body, got.Body)
	}
}

func TestRewriteAttachmentLinks(t *testing.T) {
	d := attachmentsTestDeps(t)
	id, err := d.Store.AddAttachment(store.Attachment{MIME: "image/png", Size: 3})
	if err != nil {
		t.Fatalf("AddAttachment: %v", err)
	}

	in := "see ![screenshot](/v1/attachments/" + itoa(id) + ") and ![gone](/v1/attachments/999) and [a link](https://x.test)"
	got := rewriteAttachmentLinks(d, in)

	wantPath := filepath.Join(d.Cfg.AttachmentsDir, itoa(id)+".png")
	if !strings.Contains(got, "[screenshot: "+wantPath+"]") {
		t.Errorf("known link not rewritten: %q", got)
	}
	if !strings.Contains(got, "![gone](/v1/attachments/999)") {
		t.Errorf("unknown id must stay untouched: %q", got)
	}
	if !strings.Contains(got, "[a link](https://x.test)") {
		t.Errorf("non-attachment link must stay untouched: %q", got)
	}
}
