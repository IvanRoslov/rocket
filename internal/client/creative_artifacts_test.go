package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreativeArtifactUploadStreamsRawBytesAndIdentity(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "creative-agent")
	var body, typ, sid, filename, stage string
	var deadline time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		body = string(b)
		typ = r.Header.Get("Content-Type")
		sid = r.Header.Get("X-Rocket-Session")
		filename = r.URL.Query().Get("filename")
		stage = r.URL.Query().Get("stage_id")
		w.WriteHeader(201)
		io.WriteString(w, `{"version":{"id":42}}`)
	}))
	defer srv.Close()
	c := &Client{baseURL: srv.URL, http: srv.Client()}
	var out struct {
		Version struct {
			ID int64 `json:"id"`
		} `json:"version"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	deadline, _ = ctx.Deadline()
	if err := c.UploadCreativeArtifact(ctx, "/v1/creative/runs/7/artifacts?stage_id=8", "film + final.mp4", "video/mp4", strings.NewReader("raw-video"), &out); err != nil {
		t.Fatal(err)
	}
	if body != "raw-video" || typ != "video/mp4" || sid != "creative-agent" || filename != "film + final.mp4" || stage != "8" || out.Version.ID != 42 {
		t.Fatalf("upload %q %q %q %q %q %+v", body, typ, sid, filename, stage, out)
	}
	if deadline.Before(time.Now()) {
		t.Fatal("unexpected timeout")
	}
}
func TestCreativeArtifactUploadReportsRejectionAndReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(413)
		io.WriteString(w, `{"error":{"code":"too_large","message":"artifact exceeds 2 GiB"}}`)
	}))
	defer srv.Close()
	c := &Client{baseURL: srv.URL, http: srv.Client()}
	err := c.UploadCreativeArtifact(context.Background(), "/v1/creative/runs/1/artifacts", "x.mp4", "video/mp4", strings.NewReader("x"), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "too_large" {
		t.Fatalf("rejection %v", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		io.WriteString(w, `{}`)
	}))
	defer srv2.Close()
	c.baseURL = srv2.URL
	c.http = srv2.Client()
	if err := c.UploadCreativeArtifact(context.Background(), "/v1/creative/runs/1/artifacts", "x.mp4", "video/mp4", failingArtifactUploadReader{}, nil); err == nil {
		t.Fatal("read failure became success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.UploadCreativeArtifact(ctx, "/v1/creative/runs/1/artifacts", "x.mp4", "video/mp4", strings.NewReader("x"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upload: %v", err)
	}
}

type failingArtifactUploadReader struct{}

func (failingArtifactUploadReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
