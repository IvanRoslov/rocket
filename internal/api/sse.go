package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// sseHeartbeat is how often a ": ping" comment is written to keep the
// connection alive through idle proxies. A package var so tests can shrink
// it.
var sseHeartbeat = 15 * time.Second

// sseCatchupLimit bounds how many events a catch-up (since= or
// Last-Event-ID) can replay before switching to live streaming.
const sseCatchupLimit = 1000

// registerSSERoutes wires the /v1/events/stream route onto mux.
func registerSSERoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		handleEventsStream(w, r, d)
	})
}

// handleEventsStream serves GET /v1/events/stream?session=&since=.
//
// It subscribes to the bus first (so no events are missed between
// subscribe and catch-up), then replays any requested catch-up window from
// the store, then streams live events from the bus, deduplicating against
// whatever was already sent during catch-up.
func handleEventsStream(w http.ResponseWriter, r *http.Request, d Deps) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming_unsupported", "response writer does not support flushing")
		return
	}

	sessionID := r.URL.Query().Get("session")

	// Subscribe to the bus before doing catch-up, so no event published
	// during catch-up is lost.
	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var lastSentID int64

	if since, ok := catchupSince(r); ok {
		events, err := d.Store.ListEvents(since, sseCatchupLimit, sessionID)
		if err != nil {
			// Headers are already sent; nothing better to do than stop.
			return
		}
		for _, e := range events {
			if !writeSSEEvent(w, flusher, e) {
				return
			}
			lastSentID = e.ID
		}
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			if e.ID <= lastSentID {
				continue
			}
			if sessionID != "" && e.SessionID != sessionID {
				continue
			}
			if !writeSSEEvent(w, flusher, e) {
				return
			}
			lastSentID = e.ID
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// catchupSince determines the catch-up starting point from the "since"
// query parameter (preferred) or the Last-Event-ID header. It returns
// ok=false when neither is present, meaning no catch-up should happen.
func catchupSince(r *http.Request) (since int64, ok bool) {
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// writeSSEEvent writes a single store.Event in SSE wire format and flushes.
// It returns false if the write failed (connection gone), in which case the
// caller should stop streaming.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, e store.Event) bool {
	data, err := json.Marshal(toEventResponse(e))
	if err != nil {
		return true // skip a single bad event rather than killing the stream
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
