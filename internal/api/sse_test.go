package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
)

// sseTestDeps builds Deps with a real store + bus, for tests that exercise
// the /v1/events/stream endpoint.
func sseTestDeps(t *testing.T) Deps {
	t.Helper()
	d := eventsTestDeps(t)
	d.Bus = bus.New(d.Store)
	return d
}

// sseLine is a single parsed "id:"/"event:"/"data:" triple from the SSE
// stream.
type sseLine struct {
	id    string
	event string
	data  string
}

// readSSEEvents reads from the stream until it has collected want events (or
// the deadline passes), also reporting whether a heartbeat comment was
// observed.
func readSSEStream(t *testing.T, body *bufio.Scanner, want int, deadline time.Time) (events []sseLine, sawPing bool) {
	t.Helper()
	var cur sseLine
	haveAny := false

	resultCh := make(chan struct{})
	go func() {
		defer close(resultCh)
		for body.Scan() {
			line := body.Text()
			switch {
			case line == "":
				if haveAny {
					events = append(events, cur)
					cur = sseLine{}
					haveAny = false
					if len(events) >= want {
						return
					}
				}
			case strings.HasPrefix(line, ": "):
				if strings.Contains(line, "ping") {
					sawPing = true
				}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
				haveAny = true
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
				haveAny = true
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
				haveAny = true
			}
		}
	}()

	select {
	case <-resultCh:
	case <-time.After(time.Until(deadline)):
		t.Fatalf("timed out waiting for %d SSE events, got %d: %+v", want, len(events), events)
	}
	return events, sawPing
}

func openSSE(t *testing.T, ctx context.Context, url string) (*http.Response, *bufio.Scanner) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	return resp, bufio.NewScanner(resp.Body)
}

func TestSSE_Live(t *testing.T) {
	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream")
	defer resp.Body.Close()

	// Give the handler a moment to subscribe before publishing.
	time.Sleep(50 * time.Millisecond)
	d.Bus.Publish("test.live", "s1", map[string]any{"k": "v"})

	events, _ := readSSEStream(t, scanner, 1, time.Now().Add(3*time.Second))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].event != "test.live" {
		t.Errorf("event = %q, want test.live", events[0].event)
	}
	if !strings.Contains(events[0].data, `"type":"test.live"`) {
		t.Errorf("data = %q, missing type field", events[0].data)
	}
	if events[0].id == "" {
		t.Errorf("id is empty")
	}
}

// TestSSE_ChatUpdatedFlowsThrough verifies that a session.chat_updated event
// (as published by the monitor's chat watcher) flows through the generic SSE
// stream unchanged, same as any other bus event — the chat feature needs no
// SSE-side changes.
func TestSSE_ChatUpdatedFlowsThrough(t *testing.T) {
	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream")
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)
	d.Bus.Publish("session.chat_updated", "sess1", map[string]any{})

	events, _ := readSSEStream(t, scanner, 1, time.Now().Add(3*time.Second))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].event != "session.chat_updated" {
		t.Errorf("event = %q, want session.chat_updated", events[0].event)
	}
	if !strings.Contains(events[0].data, `"session_id":"sess1"`) {
		t.Errorf("data = %q, missing session_id", events[0].data)
	}
}

func TestSSE_CatchupThenLive(t *testing.T) {
	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	// Seed store events directly (not via bus, so they predate the
	// subscription).
	if _, err := d.Store.AppendEvent(eventArg("seed.1", "s1")); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := d.Store.AppendEvent(eventArg("seed.2", "s1")); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream?since=0")
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)
	d.Bus.Publish("live.3", "s1", map[string]any{"k": "v"})

	events, _ := readSSEStream(t, scanner, 3, time.Now().Add(3*time.Second))
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	wantOrder := []string{"seed.1", "seed.2", "live.3"}
	for i, w := range wantOrder {
		if events[i].event != w {
			t.Errorf("events[%d].event = %q, want %q", i, events[i].event, w)
		}
	}
	// No duplicate ids.
	seen := map[string]bool{}
	for _, e := range events {
		if seen[e.id] {
			t.Errorf("duplicate id %q in stream", e.id)
		}
		seen[e.id] = true
	}
}

func TestSSE_SessionFilter(t *testing.T) {
	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream?session=s1")
	defer resp.Body.Close()

	time.Sleep(50 * time.Millisecond)
	d.Bus.Publish("other.session", "s2", map[string]any{})
	d.Bus.Publish("mine.session", "s1", map[string]any{})

	events, _ := readSSEStream(t, scanner, 1, time.Now().Add(3*time.Second))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].event != "mine.session" {
		t.Errorf("event = %q, want mine.session (session filter leaked)", events[0].event)
	}
}

func TestSSE_Heartbeat(t *testing.T) {
	old := sseHeartbeat
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = old }()

	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream")
	defer resp.Body.Close()

	// No events published; wait long enough for a couple of heartbeats,
	// then publish one real event so readSSEStream can return.
	time.AfterFunc(200*time.Millisecond, func() {
		d.Bus.Publish("wake", "", map[string]any{})
	})

	_, sawPing := readSSEStream(t, scanner, 1, time.Now().Add(3*time.Second))
	if !sawPing {
		t.Errorf("did not observe a heartbeat ping")
	}
}

func TestSSE_Disconnect(t *testing.T) {
	d := sseTestDeps(t)
	srv := newTestServer(t, d)

	ctx, cancel := context.WithCancel(context.Background())

	resp, scanner := openSSE(t, ctx, srv.URL+"/v1/events/stream")
	_ = scanner

	// Cancelling the request context should cause the handler to notice
	// r.Context().Done() and return, releasing its bus subscription. We
	// can't directly observe the server-side goroutine exiting, but we can
	// at least confirm the client-side read unblocks/errors out, and that
	// the server keeps serving other requests afterwards (no deadlock/leak
	// wedging the mux).
	cancel()
	resp.Body.Close()

	// Server should still be responsive.
	resp2, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health after disconnect: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
}

// sanity check that catchupSince parses since=0 as "do catch-up from 0",
// distinct from "no since given at all".
func TestCatchupSince(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/v1/events/stream?since=0", nil)
	since, ok := catchupSince(req)
	if !ok || since != 0 {
		t.Fatalf("catchupSince(since=0) = (%d, %v), want (0, true)", since, ok)
	}

	req2, _ := http.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	if _, ok := catchupSince(req2); ok {
		t.Fatalf("catchupSince(no since) ok = true, want false")
	}

	req3, _ := http.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	req3.Header.Set("Last-Event-ID", "5")
	since3, ok3 := catchupSince(req3)
	if !ok3 || since3 != 5 {
		t.Fatalf("catchupSince(Last-Event-ID=5) = (%d, %v), want (5, true)", since3, ok3)
	}
}
