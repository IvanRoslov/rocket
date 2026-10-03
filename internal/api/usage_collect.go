package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// UsageCollector is the slice of usage.Collector the API drives: every
// method only queues work for the collector's own goroutine.
type UsageCollector interface {
	Snapshot(sessionID string)
	Enqueue(sessionID string)
	EnqueueTerminal(includeCollected, includeMissing bool) (int, error)
}

type collectUsageRequest struct {
	SessionID    string `json:"session_id"`
	All          bool   `json:"all"`
	RetryMissing bool   `json:"retry_missing"`
}

type collectUsageResponse struct {
	Queued int `json:"queued"`
}

func registerUsageCollectRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/stats/usage/collect", func(w http.ResponseWriter, r *http.Request) {
		handleCollectUsage(w, r, d)
	})
}

// handleCollectUsage queues a manual re-collection: one session, all
// terminal sessions ({all}), and/or the "missing" ones ({retry_missing}),
// which the sweeper never retries on its own. Human-only.
func handleCollectUsage(w http.ResponseWriter, r *http.Request, d Deps) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "human_only",
			"only the human starts usage collection; agent session "+caller.ID+" may not")
		return
	}
	var req collectUsageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	bulk := req.All || req.RetryMissing
	if (req.SessionID == "") == !bulk {
		writeErr(w, http.StatusBadRequest, "bad_request", "pass either session_id or all/retry_missing")
		return
	}
	if d.Usage == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "usage collector is not running")
		return
	}
	if req.SessionID != "" {
		if _, err := d.Store.GetSession(req.SessionID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "session_not_found", "session not found: "+req.SessionID)
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		d.Usage.Enqueue(req.SessionID)
		writeJSON(w, http.StatusAccepted, collectUsageResponse{Queued: 1})
		return
	}
	n, err := d.Usage.EnqueueTerminal(req.All, req.RetryMissing)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, collectUsageResponse{Queued: n})
}
