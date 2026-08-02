package api

import (
	"errors"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// sessionHeader is the HTTP header agent callers set to identify themselves.
// See internal/client, which sets it from the ROCKET_SESSION_ID env var.
const sessionHeader = "X-Rocket-Session"

// errUnknownSession is returned by callerSession when the header names a
// session id that doesn't exist in the store. Handlers map this to a 401
// unknown_session response.
var errUnknownSession = errors.New("unknown session")

// callerSession resolves the caller of an API request. When the
// X-Rocket-Session header is absent or empty, the caller is a human user
// with full rights: (nil, nil) is returned. When present, the named session
// must exist; if it doesn't, errUnknownSession is returned.
func callerSession(r *http.Request, st *store.Store) (*store.Session, error) {
	id := r.Header.Get(sessionHeader)
	if id == "" {
		return nil, nil
	}
	sess, err := st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errUnknownSession
		}
		return nil, err
	}
	return &sess, nil
}

// writeCallerErr maps an error from callerSession to its HTTP response.
// Returns true if it wrote a response (caller should return immediately).
func writeCallerErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errUnknownSession) {
		writeErr(w, http.StatusUnauthorized, "unknown_session", "unknown session: "+err.Error())
		return true
	}
	writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
	return true
}

// canWriteTask reports whether caller has write access to task. caller ==
// nil means a human user, who has full rights. A registered persistent agent
// has the same rights as the human user. An orchestrator can write to its own
// task, or to a subtask whose parent task belongs to it. A worker can only
// write to its own (sub)task.
func canWriteTask(caller *store.Session, task store.Task, st *store.Store) bool {
	if caller == nil {
		return true
	}
	if caller.Kind == "agent" {
		return true
	}
	if task.SessionID == caller.ID {
		return true
	}
	if caller.Kind == "orchestrator" && task.ParentID != 0 {
		parent, err := st.GetTask(task.ParentID)
		if err != nil {
			return false
		}
		return parent.SessionID == caller.ID
	}
	return false
}
