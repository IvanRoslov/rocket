package pi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
)

var (
	_ agent.BusyInputAccepter = (*Pi)(nil)
	_ agent.DeliveryConfirmer = (*Pi)(nil)
	_ agent.PaneDialecter     = (*Pi)(nil)
	_ agent.BracketedPaster   = (*Pi)(nil)
)

func (p *Pi) AcceptsInputWhileBusy() bool { return true }

func (p *Pi) PaneDialect() string { return runtime.PaneDialectPi }

func (p *Pi) BracketedPaste() bool { return true }

func userText(rec sessionRecord) ([]string, bool) {
	if rec.Type != "message" {
		return nil, false
	}
	var envelope struct {
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(rec.Raw, &envelope) != nil || envelope.Message.Role != "user" {
		return nil, false
	}
	var parts []string
	for _, part := range envelope.Message.Content {
		if part.Type == "text" {
			parts = append(parts, part.Text)
		}
	}
	return parts, true
}

func (p *Pi) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	want := normalizeSpace(text)
	if want == "" {
		return false, time.Time{}, nil
	}
	path, err := findSession(piHome(), ref.WorktreePath, ref.SessionID)
	if err != nil {
		return false, time.Time{}, err
	}
	records, err := readActiveBranch(path)
	if err != nil {
		return false, time.Time{}, err
	}
	var at time.Time
	found := false
	for i := len(records) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return false, time.Time{}, err
		}
		rec := records[i]
		if !rec.Timestamp.IsZero() && !rec.Timestamp.After(since) {
			break
		}
		parts, ok := userText(rec)
		if !ok || rec.Timestamp.IsZero() || !rec.Timestamp.After(since) {
			continue
		}
		for _, part := range append(parts, strings.Join(parts, "\n")) {
			if holdsLines(part, want) {
				at, found = rec.Timestamp, true
				break
			}
		}
	}
	return found, at, nil
}

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

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
