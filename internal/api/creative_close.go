package api

import (
	"errors"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

func handleCreativeClose(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session) {
	runID, ok := creativeID(w, r.PathValue("run"), false)
	if !ok {
		return
	}
	run, err := d.Store.GetCreativeRun(runID)
	if err != nil {
		creativeError(w, err)
		return
	}
	if !isSpawningOrchestrator(caller) || caller.ID != run.OrchestratorSessionID {
		writeErr(w, http.StatusForbidden, "creative_forbidden", "only the live run orchestrator may close workers")
		return
	}
	if !creativeCloseSessionBound(w, d, *caller, run) {
		return
	}
	target, err := d.Store.GetSession(r.PathValue("session"))
	if err != nil {
		creativeError(w, err)
		return
	}
	if target.Kind != "worker" {
		writeErr(w, http.StatusForbidden, "creative_forbidden", "only this run's workers may be closed")
		return
	}
	if !creativeCloseSessionBound(w, d, target, run) {
		return
	}
	if d.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "runtime_unavailable", "session manager is not running")
		return
	}
	// Reuse normal session stop, including terminal idempotency, without cleanup
	// or cascade. Ownership follows the run, even after orchestrator replacement.
	if err := d.Manager.Kill(r.Context(), target.ID, false); err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func creativeCloseSessionBound(w http.ResponseWriter, d Deps, sess store.Session, run store.CreativeRun) bool {
	b, err := d.Store.GetCreativeSessionContext(sess.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		creativeError(w, err)
		return false
	}
	if err != nil || b.RunID != run.ID || sess.ProjectID != run.ProjectID || sess.TaskID != 0 || sess.SubtaskID != 0 {
		writeErr(w, http.StatusForbidden, "creative_forbidden", "session must belong to this Creative run and project")
		return false
	}
	return true
}
