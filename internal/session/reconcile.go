package session

import (
	"context"
	"errors"
	"os"
	"regexp"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Reconcile compares the store's sessions against the live runtime and
// worktree directories, marking any mismatches as errored and publishing
// appropriate events. It also reports orphan tmux sessions (live but not
// in store) as a single event. Errors from store/runtime are collected
// and returned joined, but reconciliation continues for other sessions.
func (m *Manager) Reconcile(ctx context.Context) error {
	// List all live tmux sessions from runtime
	liveNames, err := m.rt.List(ctx)
	if err != nil {
		return err
	}

	// Build a map of live session names for O(1) lookup
	liveSet := make(map[string]bool, len(liveNames))
	for _, name := range liveNames {
		liveSet[name] = true
	}

	// List sessions from store that are not in terminal state
	sessions, err := m.st.ListSessions(store.SessionFilter{All: false})
	if err != nil {
		return err
	}

	// Track which live names correspond to stored sessions
	storedSet := make(map[string]bool, len(sessions))

	// Collect errors but continue processing
	var errs []error

	// Check each non-terminal session
	for _, sess := range sessions {
		storedSet[sess.TmuxName] = true

		// Check if tmux session exists
		if !liveSet[sess.TmuxName] {
			// Tmux session is missing: mark errored and publish event
			if err := m.st.UpdateSessionState(sess.ID, "errored"); err != nil {
				errs = append(errs, err)
				continue
			}
			m.bus.Publish("session.state_changed", sess.ID, map[string]any{
				"from": sess.State, "to": "errored", "reason": "tmux_missing",
			})
			continue
		}

		// Tmux session exists; check if worktree directory exists
		if _, err := os.Stat(sess.WorktreePath); err != nil {
			if !os.IsNotExist(err) {
				// Some other error (permission, I/O, etc.); collect it but don't fail
				errs = append(errs, err)
				continue
			}
			// Worktree directory is missing: mark errored and publish event
			// Note: we do NOT kill the tmux session
			if err := m.st.UpdateSessionState(sess.ID, "errored"); err != nil {
				errs = append(errs, err)
				continue
			}
			m.bus.Publish("session.state_changed", sess.ID, map[string]any{
				"from": sess.State, "to": "errored", "reason": "worktree_missing",
			})
		}
		// Session is healthy (tmux alive, worktree present): no action
	}

	// Find orphan tmux sessions: live but not in store
	orphans := findOrphans(liveNames, storedSet)
	if len(orphans) > 0 {
		m.bus.Publish("reconcile.orphan_tmux", "", map[string]any{
			"names": orphans,
		})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// findOrphans returns a slice of tmux session names that:
// 1. Match the pattern ^[a-z0-9-]+$ (look like rocket sessions)
// 2. Are not in the storedSet
func findOrphans(liveNames []string, storedSet map[string]bool) []string {
	pattern := regexp.MustCompile(`^[a-z0-9-]+$`)
	var orphans []string

	for _, name := range liveNames {
		if pattern.MatchString(name) && !storedSet[name] {
			orphans = append(orphans, name)
		}
	}

	return orphans
}
