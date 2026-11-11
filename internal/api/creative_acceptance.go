package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

func registerCreativeAcceptanceRoute(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/creative/runs/{run}/accept", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		if caller != nil {
			creativeError(w, store.ErrCreativeForbidden)
			return
		}
		id, ok := creativeID(w, r.PathValue("run"), false)
		if !ok {
			return
		}
		if d.Manager == nil {
			writeErr(w, 503, "runtime_unavailable", "session manager is not running; closure has not completed")
			return
		}
		run, err := d.Manager.AcceptCreativeRun(r.Context(), id)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, run)
	}))
}

// Apply the barrier to existing generic service routes too. Persistence checks
// serialize work writes with the acceptance decision; this guard also covers
// runtime-only operations (terminal, quiz, attach, restore and cleanup).
func creativeArchiveBoundary(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		mutation := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if mutation {
			caller, err := callerSession(r, d.Store)
			if writeCallerErr(w, err) {
				return
			}
			if caller != nil {
				if err = d.Store.CheckCreativeSessionMutable(caller.ID); err != nil {
					creativeError(w, err)
					return
				}
			}
		}
		if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "sessions" && (mutation || (len(parts) >= 4 && (parts[3] == "term" || parts[3] == "attach"))) {
			if err := d.Store.CheckCreativeSessionMutable(parts[2]); err != nil {
				creativeError(w, err)
				return
			}
		}
		if mutation && len(parts) >= 4 && parts[0] == "v1" && parts[1] == "creative" && parts[2] == "runs" {
			// Acceptance and harmless reader-position updates are the only writes
			// allowed after the freeze. All run material and agent writes stay closed.
			if !(len(parts) == 5 && parts[4] == "accept") && !strings.HasSuffix(r.URL.Path, "/read") {
				if id, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
					if err = d.Store.CheckCreativeRunMutable(id); err != nil {
						creativeError(w, err)
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func creativeSessionWriteAllowed(w http.ResponseWriter, d Deps, id string) bool {
	if err := d.Store.CheckCreativeSessionMutable(id); err != nil && !errors.Is(err, store.ErrNotFound) {
		creativeError(w, err)
		return false
	}
	return true
}
