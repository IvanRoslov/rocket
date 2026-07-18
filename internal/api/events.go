package api

import (
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/store"
)

// defaultEventsLimit and maxEventsLimit bound the "limit" query parameter
// on GET /v1/events.
const (
	defaultEventsLimit = 100
	maxEventsLimit     = 1000
)

// eventResponse is the JSON shape of an event as returned by the API.
type eventResponse struct {
	ID        int64          `json:"id"`
	TS        int64          `json:"ts"`
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

func toEventResponse(e store.Event) eventResponse {
	return eventResponse{
		ID:        e.ID,
		TS:        e.TS,
		Type:      e.Type,
		SessionID: e.SessionID,
		Data:      e.Data,
	}
}

// registerEventsRoutes wires the /v1/events route onto mux.
func registerEventsRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/events", func(w http.ResponseWriter, r *http.Request) {
		handleListEvents(w, r, d)
	})
}

// handleListEvents serves GET /v1/events?since=<id>&limit=N&session=<id>.
//
// When "since" is given (and >0), it returns events with id > since, up to
// limit, ascending. This is the polling shape used by `rocket events
// --follow`.
//
// When "since" is absent, it returns the *tail* of the log: the last limit
// events, ascending. This is the one-shot "show me recent activity" shape.
func handleListEvents(w http.ResponseWriter, r *http.Request, d Deps) {
	q := r.URL.Query()

	limit := defaultEventsLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxEventsLimit {
		limit = maxEventsLimit
	}

	sessionID := q.Get("session")

	var (
		events []store.Event
		err    error
	)
	if sinceStr := q.Get("since"); sinceStr != "" {
		since, perr := strconv.ParseInt(sinceStr, 10, 64)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid since parameter")
			return
		}
		events, err = d.Store.ListEvents(since, limit, sessionID)
	} else {
		events, err = d.Store.ListEventsTail(limit, sessionID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]eventResponse, len(events))
	for i, e := range events {
		out[i] = toEventResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
