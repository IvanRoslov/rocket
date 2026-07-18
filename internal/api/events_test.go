package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// eventArg builds a store.Event for AppendEvent in tests.
func eventArg(typ, sessionID string) store.Event {
	return store.Event{Type: typ, SessionID: sessionID, Data: map[string]any{"k": "v"}}
}

// eventsTestDeps builds Deps with a real store on a temp SQLite db, for
// tests that exercise the /v1/events endpoint.
func eventsTestDeps(t *testing.T) Deps {
	t.Helper()
	// Reuse the same store-setup helper as the repos tests.
	return reposTestDeps(t)
}

type eventResp struct {
	ID        int64          `json:"id"`
	TS        int64          `json:"ts"`
	Type      string         `json:"type"`
	SessionID string         `json:"session_id"`
	Data      map[string]any `json:"data"`
}

func decodeEventsResp(t *testing.T, resp *http.Response) []eventResp {
	t.Helper()
	defer resp.Body.Close()
	var body struct {
		Events []eventResp `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	return body.Events
}

func TestListEvents_Basic(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	for i := 0; i < 3; i++ {
		if _, err := d.Store.AppendEvent(eventArg(fmt.Sprintf("type%d", i), "s1")); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	if _, err := d.Store.AppendEvent(eventArg("other.type", "s2")); err != nil {
		t.Fatalf("AppendEvent s2: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/events")
	if err != nil {
		t.Fatalf("GET /v1/events: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}
	if events[0].Type != "type0" {
		t.Fatalf("events[0].Type = %q, want type0", events[0].Type)
	}
}

func TestListEvents_Since(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	var firstID int64
	for i := 0; i < 3; i++ {
		id, err := d.Store.AppendEvent(eventArg(fmt.Sprintf("type%d", i), "s1"))
		if err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		if i == 0 {
			firstID = id
		}
	}

	resp, err := http.Get(fmt.Sprintf("%s/v1/events?since=%d", srv.URL, firstID))
	if err != nil {
		t.Fatalf("GET /v1/events?since=: %v", err)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Type != "type1" {
		t.Fatalf("events[0].Type = %q, want type1", events[0].Type)
	}
}

func TestListEvents_Limit(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	for i := 0; i < 5; i++ {
		if _, err := d.Store.AppendEvent(eventArg(fmt.Sprintf("type%d", i), "s1")); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	resp, err := http.Get(srv.URL + "/v1/events?limit=2")
	if err != nil {
		t.Fatalf("GET /v1/events?limit=2: %v", err)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
}

func TestListEvents_Session(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	if _, err := d.Store.AppendEvent(eventArg("t1", "s1")); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := d.Store.AppendEvent(eventArg("t2", "s2")); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/events?session=s2")
	if err != nil {
		t.Fatalf("GET /v1/events?session=s2: %v", err)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 1 || events[0].SessionID != "s2" {
		t.Fatalf("events = %+v, want single s2 event", events)
	}
}

func TestListEvents_Tail(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	for i := 0; i < 5; i++ {
		if _, err := d.Store.AppendEvent(eventArg(fmt.Sprintf("type%d", i), "s1")); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	// No "since" given, only "limit": server returns the tail (last N),
	// ascending by id.
	resp, err := http.Get(srv.URL + "/v1/events?limit=2")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Type != "type3" || events[1].Type != "type4" {
		t.Fatalf("events = %+v, want tail [type3, type4]", events)
	}
}

func TestListEvents_DefaultLimit(t *testing.T) {
	d := eventsTestDeps(t)
	srv := newTestServer(t, d)

	for i := 0; i < 150; i++ {
		if _, err := d.Store.AppendEvent(eventArg(fmt.Sprintf("type%d", i), "s1")); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	resp, err := http.Get(srv.URL + "/v1/events")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	events := decodeEventsResp(t, resp)
	if len(events) != 100 {
		t.Fatalf("len(events) = %d, want default 100", len(events))
	}
}
