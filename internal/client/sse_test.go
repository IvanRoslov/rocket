package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sseEvent struct {
	id    int64
	event string
	data  string
}

func TestParseSSE_BasicEvents(t *testing.T) {
	raw := "id: 1\nevent: foo\ndata: {\"a\":1}\n\n" +
		"id: 2\nevent: bar\ndata: {\"a\":2}\n\n"

	var got []sseEvent
	err := parseSSE(strings.NewReader(raw), func(id int64, event, data string) error {
		got = append(got, sseEvent{id, event, data})
		return nil
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != (sseEvent{1, "foo", `{"a":1}`}) {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1] != (sseEvent{2, "bar", `{"a":2}`}) {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestParseSSE_IgnoresComments(t *testing.T) {
	raw := ": ping\n\nid: 1\nevent: foo\ndata: {}\n\n: ping\n\n"

	var got []sseEvent
	err := parseSSE(strings.NewReader(raw), func(id int64, event, data string) error {
		got = append(got, sseEvent{id, event, data})
		return nil
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (comments must not dispatch)", len(got))
	}
}

func TestParseSSE_HandlerErrorStopsStream(t *testing.T) {
	raw := "id: 1\nevent: foo\ndata: {}\n\nid: 2\nevent: bar\ndata: {}\n\n"

	boom := fmt.Errorf("boom")
	callCount := 0
	err := parseSSE(strings.NewReader(raw), func(id int64, event, data string) error {
		callCount++
		return boom
	})
	if err != boom {
		t.Fatalf("err = %v, want boom", err)
	}
	if callCount != 1 {
		t.Fatalf("callCount = %d, want 1 (must stop after first error)", callCount)
	}
}

func TestClientStream_AgainstHTTPServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept header = %q, want text/event-stream", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "id: 1\nevent: hello\ndata: {\"msg\":\"hi\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, http: srv.Client()}

	var got []sseEvent
	err := c.Stream(context.Background(), "/stream", func(id int64, event, data string) error {
		got = append(got, sseEvent{id, event, data})
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 1 || got[0].event != "hello" {
		t.Fatalf("got = %+v", got)
	}
}

func TestClientStream_NonOKStatusReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"code":"not_found","message":"nope"}}`)
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, http: srv.Client()}

	err := c.Stream(context.Background(), "/stream", func(id int64, event, data string) error {
		t.Fatalf("handler should not be called on non-OK status")
		return nil
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "not_found" {
		t.Errorf("code = %q, want not_found", apiErr.Code)
	}
}
