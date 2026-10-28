package kimicode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// freshWindow is how recent the journal's mtime must be for a session
// without an open turn to count as working. Same threshold as codex.
const freshWindow = 30 * time.Second

// turnState is what the tail of a main journal says about its latest turn.
type turnState struct {
	open            bool   // last turn record is turn.prompt
	failed          bool   // last turn ended with reason failed or blocked
	errText         string // error.message of that failed turn
	turnID          int64  // turnId of the last turn.ended
	openInteraction bool   // an interaction.request after the last turn boundary is unresolved
	found           bool   // any turn record or interaction was seen
}

// readTurnState scans the journal from its end back to the newest turn
// boundary (turn.prompt or turn.ended).
func readTurnState(path string) (turnState, error) {
	var st turnState
	resolved := map[string]bool{}
	_, err := readTail(path, func(rec wireRecord) bool {
		switch rec.Type {
		case "interaction.resolved":
			var r struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(rec.Raw, &r) == nil {
				resolved[r.ID] = true
			}
		case "interaction.request":
			var r struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(rec.Raw, &r) == nil && !resolved[r.ID] {
				st.openInteraction = true
			}
			st.found = true
		case "turn.prompt":
			st.open, st.found = true, true
			return true
		case "turn.ended":
			var r struct {
				TurnID int64  `json:"turnId"`
				Reason string `json:"reason"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(rec.Raw, &r) == nil && (r.Reason == "failed" || r.Reason == "blocked") {
				st.failed, st.turnID = true, r.TurnID
				if r.Error != nil {
					st.errText = r.Error.Message
				}
			}
			st.found = true
			return true
		}
		return false
	})
	return st, err
}

var _ agent.TurnFailureReporter = (*KimiCode)(nil)

// LastTurnFailure implements agent.TurnFailureReporter: the latest turn of
// the session's main journal ended failed or blocked. The turn ID is
// «<session dir>/<turnId>», since Kimi numbers turns per session.
func (k *KimiCode) LastTurnFailure(ctx context.Context, ref agent.ActivityRef) (string, string, bool, error) {
	dir, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return "", "", false, agent.ErrNoSignal
	}
	st, err := readTurnState(mainWire(dir))
	if err != nil {
		return "", "", false, agent.ErrNoSignal
	}
	if !st.failed {
		return "", "", false, nil
	}
	return filepath.Base(dir) + "/" + strconv.FormatInt(st.turnID, 10), st.errText, true, nil
}

// Activity implements agent.Agent over the main journal of the newest Kimi
// session of ref.WorktreePath created at or after ref.Since. Kimi writes no
// journal until the first prompt, so before it the answer is ErrNoSignal and
// input readiness comes from the pane (runtime.ParseKimiComposer).
//
// A turn that ended failed/blocked (quota exhaustion included) or an open
// interaction.request is Blocked; an open turn is Active whatever the mtime
// (a long tool call writes nothing); otherwise a journal written within
// freshWindow is Active and an older one Ready.
func (k *KimiCode) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	dir, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}
	path := mainWire(dir)
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}
	mtime := info.ModTime()
	st, err := readTurnState(path)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}
	switch {
	case st.failed, st.openInteraction:
		return activity.Blocked, mtime, nil
	case st.open:
		return activity.Active, mtime, nil
	case time.Since(mtime) < freshWindow:
		return activity.Active, mtime, nil
	}
	return activity.Ready, mtime, nil
}
