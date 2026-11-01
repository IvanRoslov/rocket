package kimicode

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
)

var (
	_ agent.BusyInputAccepter = (*KimiCode)(nil)
	_ agent.DeliveryConfirmer = (*KimiCode)(nil)
	_ agent.PaneDialecter     = (*KimiCode)(nil)
)

// AcceptsInputWhileBusy implements agent.BusyInputAccepter: Enter during a
// turn puts the text into Kimi's own queue (the «❯ …» row above the
// composer) and Kimi starts a turn for it when the current one ends.
func (k *KimiCode) AcceptsInputWhileBusy() bool { return true }

// PaneDialect implements agent.PaneDialecter: Kimi's composer box and
// unnumbered start screens need the Kimi pane parsers.
func (k *KimiCode) PaneDialect() string { return runtime.PaneDialectKimi }

// userPrompt decodes rec as a user-origin turn.prompt and returns its text
// parts. The startup prober uses it to recognize the first turn; a steer is
// not a new turn.
func userPrompt(rec wireRecord) ([]string, bool) {
	if rec.Type != "turn.prompt" {
		return nil, false
	}
	return userOriginParts(rec)
}

// userInput decodes rec as input Kimi actually received from the user: a
// turn.prompt at the start of a new turn or a turn.steer inside the running
// turn. Skill-activation steers and system-triggered prompts (sub-agent
// briefs) are not user input even when their text matches.
func userInput(rec wireRecord) ([]string, bool) {
	switch rec.Type {
	case "turn.prompt", "turn.steer":
	default:
		return nil, false
	}
	return userOriginParts(rec)
}

// userOriginParts returns the text parts of a user-origin record.
func userOriginParts(rec wireRecord) ([]string, bool) {
	var r struct {
		Input []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"input"`
		Origin struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	}
	if json.Unmarshal(rec.Raw, &r) != nil || r.Origin.Kind != "user" {
		return nil, false
	}
	var parts []string
	for _, in := range r.Input {
		if in.Type == "text" {
			parts = append(parts, in.Text)
		}
	}
	return parts, true
}

// hasTime reports whether the record carried a time field.
func hasTime(rec wireRecord) bool { return rec.Time.UnixMilli() != 0 }

// ConfirmDelivered implements agent.DeliveryConfirmer over the main journal
// of the newest session of ref.WorktreePath. A message counts as delivered
// once a user-origin turn.prompt or turn.steer recorded strictly after since
// carries its text (whitespace-normalized), or a run of whole lines of it
// does. Kimi records a queued message only when it receives it: as a
// turn.prompt when a new turn starts, or as a turn.steer when the queue is
// steered into the running turn. Text still waiting in Kimi's own queue has
// no such record yet and is not delivery.
//
// The journal is read backwards and the scan stops at the first record
// stamped at or before since.
func (k *KimiCode) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	want := normalizeSpace(text)
	if want == "" {
		return false, time.Time{}, nil
	}
	dir, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return false, time.Time{}, agent.ErrNoSignal
	}
	var at time.Time
	found := false
	_, err = readTail(mainWire(dir), func(rec wireRecord) bool {
		if hasTime(rec) && !rec.Time.After(since) {
			return true
		}
		parts, ok := userInput(rec)
		if !ok {
			return false
		}
		for _, p := range append(parts, strings.Join(parts, "\n")) {
			if holdsLines(p, want) {
				// Newest first: keep overwriting to end at the earliest.
				at, found = rec.Time, true
				break
			}
		}
		return ctx.Err() != nil
	})
	if err != nil {
		return false, time.Time{}, agent.ErrNoSignal
	}
	if err := ctx.Err(); err != nil {
		return false, time.Time{}, err
	}
	return found, at, nil
}

// holdsLines reports whether recorded, or a run of its whole lines, equals
// want once whitespace is normalized (same rule as the codex adapter).
func holdsLines(recorded, want string) bool {
	if !strings.Contains(normalizeSpace(recorded), want) {
		return false
	}
	lines := strings.Split(recorded, "\n")
	for i := range lines {
		for j := i + 1; j <= len(lines); j++ {
			got := normalizeSpace(strings.Join(lines[i:j], "\n"))
			if got == want {
				return true
			}
			if len(got) > len(want) {
				break
			}
		}
	}
	return false
}

// normalizeSpace collapses every run of whitespace to one space and trims.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
