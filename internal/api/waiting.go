package api

import (
	"encoding/json"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// waitingTerminalAfter is how long a session may sit on interactive input
// before it is flagged. It is a display threshold and nothing else — nobody
// is messaged, nudged or escalated when it passes — so it is a constant here
// rather than a config knob.
const waitingTerminalAfter = 10 * time.Minute

// waitingTerminal is the set of live session ids currently stalled on
// interactive input, keyed by session id. The flag it feeds — a task's or a
// session's "waiting_terminal" — is derived on every read and never
// persisted: it is a function of the session's activity and the clock, so
// storing it would only give us a stale copy to keep in sync.
type waitingTerminal map[string]bool

// waitingTerminalSessions computes the stalled set over all live sessions in
// one query. Dead sessions are excluded by construction: nobody is waiting at
// a terminal that no longer exists.
func waitingTerminalSessions(d Deps) (waitingTerminal, error) {
	sessions, err := d.Store.ListSessions(store.SessionFilter{})
	if err != nil {
		return nil, err
	}
	return waitingTerminalOf(sessions, time.Now(), waitingTerminalAfter), nil
}

// waitingTerminalOf is the pure core of waitingTerminalSessions.
func waitingTerminalOf(sessions []store.Session, now time.Time, threshold time.Duration) waitingTerminal {
	out := make(waitingTerminal, len(sessions))
	for _, s := range sessions {
		if sessionWaitingTerminal(s, now, threshold) {
			out[s.ID] = true
		}
	}
	return out
}

// sessionWaitingTerminal reports whether s is a live session that has been
// stalled on interactive input for longer than threshold. A session is
// stalled while it holds a pending AskUserQuestion quiz, or while its
// activity is waiting_input — in both cases nothing moves until somebody
// types. One that is no longer running can't be waiting on a keystroke,
// however old its last activity is.
func sessionWaitingTerminal(s store.Session, now time.Time, threshold time.Duration) bool {
	if s.State != "spawning" && s.State != "running" {
		return false
	}
	ref := waitingTerminalRef(s)
	if ref <= 0 {
		return false
	}
	return now.Sub(time.Unix(ref, 0)) > threshold
}

// waitingTerminalRef returns the timestamp the wait is measured from, or 0
// when the session is not waiting on input at all. With a quiz open it is the
// quiz's asked_at — the activity timestamp keeps moving while a quiz is up,
// so it would understate the wait. Without a usable reference point the
// session counts as not waiting rather than waiting since the epoch.
func waitingTerminalRef(s store.Session) int64 {
	switch {
	case s.PendingQuiz != "":
		var quiz session.Quiz
		if err := json.Unmarshal([]byte(s.PendingQuiz), &quiz); err == nil && quiz.AskedAt > 0 {
			return quiz.AskedAt
		}
		return s.ActivityTS
	case activity.State(s.Activity) == activity.WaitingInput:
		return s.ActivityTS
	default:
		return 0
	}
}

// annotateWaitingTerminal flags tr when its session is in the stalled set.
func annotateWaitingTerminal(tr *taskResponse, waiting waitingTerminal) {
	tr.WaitingTerminal = tr.SessionID != "" && waiting[tr.SessionID]
}
