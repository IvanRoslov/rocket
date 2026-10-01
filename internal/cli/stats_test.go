package cli

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStatsBrainstormUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"zero weeks", []string{"brainstorm", "--weeks", "0"}},
		{"too many weeks", []string{"brainstorm", "--weeks", "521"}},
		{"extra arg", []string{"brainstorm", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStatsCmd()
			cmd.SetArgs(tt.args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			err := cmd.Execute()
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

func TestRootHasStatsBrainstorm(t *testing.T) {
	for _, c := range NewRootCmd().Commands() {
		if c.Name() != "stats" {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == "brainstorm" {
				return
			}
		}
	}
	t.Fatal("rocket has no stats brainstorm subcommand")
}

func TestAcceptedShare(t *testing.T) {
	tests := []struct {
		accepted, answered int
		want               string
	}{
		{6, 10, "6 (60%)"},
		{2, 3, "2 (67%)"},
		{0, 4, "0 (0%)"},
		{0, 0, "0 (—)"},
	}
	for _, tt := range tests {
		if got := acceptedShare(tt.accepted, tt.answered); got != tt.want {
			t.Errorf("acceptedShare(%d, %d) = %q, want %q", tt.accepted, tt.answered, got, tt.want)
		}
	}
}

func TestRenderBrainstormStats(t *testing.T) {
	goAt := time.Date(2026, 10, 1, 15, 4, 0, 0, time.Local).Unix()
	stats := brainstormStats{
		Weeks: []brainstormWeekRow{
			{Week: "2026-W40", Skill: "orchestrator-brainstorming", Answered: 10, Accepted: 6, AcceptedWithComment: 3, Corrected: 3, WrongTurn: 1},
			{Week: "2026-W40", Skill: "unknown", Answered: 0},
		},
		Storms: []brainstormStormRow{
			{TaskID: 4901, Title: "Брейншторм", Skill: "orchestrator-brainstorming", Questions: 5, Answered: 5,
				Accepted: 3, AcceptedWithComment: 2, Corrected: 1, WrongTurn: 1, SpecChanges: 2, GoAt: &goAt},
			{TaskID: 4950, Title: "Без Go", Skill: "unknown", SpecChanges: 1},
		},
	}
	out := renderBrainstormStats(stats, 12)
	lines := strings.Split(out, "\n")
	wantLines := []string{
		"Брейншторм по неделям (последние 12 нед.)",
		"2026-W40  orchestrator-brainstorming  10        6 (60%)  3",
		"2026-W40  unknown                     0         0 (—)",
		"Штормы",
		"#4901",
		"2026-10-01 15:04",
		"Брейншторм",
		"#4950",
	}
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	var stormLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "#4950") {
			stormLine = l
		}
	}
	if !strings.Contains(stormLine, "—") {
		t.Errorf("storm without Go must show —: %q", stormLine)
	}

	empty := renderBrainstormStats(brainstormStats{}, 4)
	for _, want := range []string{"последние 4 нед.", "ответов на вопросы шторма нет", "штормов нет"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty output missing %q:\n%s", want, empty)
		}
	}
}
