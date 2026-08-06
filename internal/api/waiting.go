package api

import (
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/heartbeat"
	"github.com/IvanRoslov/rocket/internal/store"
)

// waitingTerminal is the set of live session ids currently stalled on
// interactive input, keyed by session id. The flag it feeds — a task's or a
// session's "waiting_terminal" — is derived on every read and never
// persisted: it is a function of the session's activity and the clock, so
// storing it would only give us a stale copy to keep in sync.
type waitingTerminal map[string]bool

// inputStallThreshold returns the configured stall threshold, falling back to
// the built-in default when Deps carries no (or a zero) config.
func inputStallThreshold(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.InputStallThreshold > 0 {
		return cfg.InputStallThreshold
	}
	return config.DefaultInputStallThreshold
}

// waitingTerminalSessions computes the stalled set over all live sessions in
// one query. Dead sessions are excluded by construction: nobody is waiting at
// a terminal that no longer exists.
func waitingTerminalSessions(d Deps) (waitingTerminal, error) {
	sessions, err := d.Store.ListSessions(store.SessionFilter{})
	if err != nil {
		return nil, err
	}
	return waitingTerminalOf(sessions, time.Now(), inputStallThreshold(d.Cfg)), nil
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

// sessionWaitingTerminal reports whether s is a live session stalled on
// interactive input. A session that is no longer running can't be waiting on
// a keystroke, however old its last activity is.
func sessionWaitingTerminal(s store.Session, now time.Time, threshold time.Duration) bool {
	if s.State != "spawning" && s.State != "running" {
		return false
	}
	_, stalled := heartbeat.InputStalled(s, now, threshold)
	return stalled
}

// annotateWaitingTerminal flags tr when its session is in the stalled set.
func annotateWaitingTerminal(tr *taskResponse, waiting waitingTerminal) {
	tr.WaitingTerminal = tr.SessionID != "" && waiting[tr.SessionID]
}
