package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/store"
)

// registerInternalActivityRoutes wires the /v1/internal/activity route onto
// mux. This endpoint is the push side of the activity channel: agent hooks
// (e.g. Claude Code's PreToolUse/Stop hooks) POST here to report their
// current state without waiting for the polling cascade.
func registerInternalActivityRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/internal/activity", func(w http.ResponseWriter, r *http.Request) {
		handlePostInternalActivity(w, r, d)
	})
}

type postInternalActivityRequest struct {
	Session string `json:"session"`
	State   string `json:"state"`
	TS      int64  `json:"ts"`
}

// handlePostInternalActivity handles POST /v1/internal/activity
// {"session":"...","state":"...","ts":<unix seconds, optional>}. The
// session must already exist (404 session_not_found), the state must be a
// valid activity.State (400 invalid_state). ts defaults to now. On success
// it forwards the update to Deps.Monitor.PushUpdate and responds 204. If
// Deps.Monitor is nil (e.g. some test wiring), it responds 503
// monitor_unavailable.
func handlePostInternalActivity(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postInternalActivityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	state := activity.State(req.State)
	if !state.Valid() {
		writeErr(w, http.StatusBadRequest, "invalid_state", "invalid activity state")
		return
	}

	if d.Monitor == nil {
		writeErr(w, http.StatusServiceUnavailable, "monitor_unavailable", "activity monitor unavailable")
		return
	}

	if _, err := d.Store.GetSession(req.Session); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	ts := time.Now()
	if req.TS > 0 {
		ts = time.Unix(req.TS, 0)
	}

	d.Monitor.PushUpdate(req.Session, state, ts)

	w.WriteHeader(http.StatusNoContent)
}
