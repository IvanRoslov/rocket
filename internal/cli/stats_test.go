package cli

import (
	"errors"
	"reflect"
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

func TestParticipantLabel(t *testing.T) {
	for id, want := range map[string]string{"human": "Иван", "": "Иван", "cto": "cto", "architect": "architect"} {
		if got := participantLabel(id); got != want {
			t.Errorf("participantLabel(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestStormWho(t *testing.T) {
	tests := []struct {
		ids  []string
		want string
	}{
		{nil, "—"},
		{[]string{}, "—"},
		{[]string{"cto"}, "cto"},
		{[]string{"human"}, "Иван"},
		{[]string{"human", "cto"}, "Иван + cto"},
		{[]string{"cto", "human", "architect"}, "cto + Иван + architect"},
	}
	for _, tt := range tests {
		if got := stormWho(tt.ids); got != tt.want {
			t.Errorf("stormWho(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
}

func TestGateState(t *testing.T) {
	goAt := int64(1790926157)
	tests := []struct {
		name        string
		goAt        *int64
		specChanges int
		hasGate     bool
		want        string
	}{
		{"go on the first try", &goAt, 0, true, "Go с 1-го раза"},
		{"go after changes", &goAt, 3, true, "Go после 3 правок"},
		{"waiting for go", nil, 2, true, "ждёт Go (правок: 2)"},
		{"waiting, no changes yet", nil, 0, true, "ждёт Go (правок: 0)"},
		{"no gates", nil, 0, false, "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gateState(tt.goAt, tt.specChanges, tt.hasGate); got != tt.want {
				t.Errorf("gateState = %q, want %q", got, tt.want)
			}
		})
	}
}

// fields is a rendered table line split on its column padding.
func fields(line string) []string {
	var out []string
	for _, f := range strings.Split(line, "  ") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// lineWithPrefix is the first output line starting with prefix.
func lineWithPrefix(t *testing.T, out, prefix string) []string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return fields(l)
		}
	}
	t.Fatalf("no line starting with %q:\n%s", prefix, out)
	return nil
}

func TestRenderBrainstormStats(t *testing.T) {
	goAt := time.Date(2026, 10, 1, 15, 4, 0, 0, time.Local).Unix()
	stats := brainstormStats{
		Weeks: []brainstormWeekRow{
			{Week: "2026-W40", Skill: "orchestrator-brainstorming", AnsweredBy: "human", Answered: 10, Accepted: 6, AcceptedWithComment: 3, Corrected: 3, WrongTurn: 1},
			{Week: "2026-W40", Skill: "orchestrator-brainstorming", AnsweredBy: "cto", Answered: 5, Accepted: 5},
			{Week: "2026-W41", Skill: "unknown", AnsweredBy: "human", Answered: 0},
		},
		Storms: []brainstormStormRow{
			{TaskID: 4901, Title: "Брейншторм", Skill: "orchestrator-brainstorming", Questions: 5, Answered: 5,
				Accepted: 3, AcceptedWithComment: 2, Corrected: 1, WrongTurn: 1, AnsweredBy: []string{"human", "cto"},
				SpecChanges: 2, HasGate: true, GoAt: &goAt},
			{TaskID: 5010, Title: "Агент", Skill: "orchestrator-brainstorming", Questions: 3, Answered: 3, Accepted: 3,
				AnsweredBy: []string{"cto"}, FirstTryGo: true, HasGate: true, GoAt: &goAt},
			{TaskID: 4950, Title: "Без Go", Skill: "unknown", AnsweredBy: []string{}, SpecChanges: 1, HasGate: true},
			{TaskID: 4960, Title: "Без гейта", Skill: "unknown", AnsweredBy: []string{}},
		},
	}
	out := renderBrainstormStats(stats, 12)
	if !strings.Contains(out, "Брейншторм по неделям (последние 12 нед.)") || !strings.Contains(out, "\nШтормы\n") {
		t.Errorf("missing section titles:\n%s", out)
	}

	checks := []struct {
		prefix string
		want   []string
	}{
		{"НЕДЕЛЯ", []string{"НЕДЕЛЯ", "СКИЛЛ", "КТО", "ОТВЕЧЕНО", "ПРИНЯТО", "С КОММЕНТАРИЕМ", "ПОПРАВЛЕНО", "НЕ ТУДА"}},
		{"2026-W40  orchestrator-brainstorming  Иван", []string{"2026-W40", "orchestrator-brainstorming", "Иван", "10", "6 (60%)", "3", "3", "1"}},
		{"2026-W40  orchestrator-brainstorming  cto", []string{"2026-W40", "orchestrator-brainstorming", "cto", "5", "5 (100%)", "0", "0", "0"}},
		{"2026-W41", []string{"2026-W41", "unknown", "Иван", "0", "0 (—)", "0", "0", "0"}},
		{"ЗАДАЧА", []string{"ЗАДАЧА", "СКИЛЛ", "КТО ШТОРМИЛ", "ВОПРОСОВ", "ОТВЕЧЕНО", "ПРИНЯТО", "С КОММЕНТАРИЕМ",
			"ПОПРАВЛЕНО", "НЕ ТУДА", "ПРАВОК ДО GO", "ГЕЙТ", "GO", "НАЗВАНИЕ"}},
		{"#4901", []string{"#4901", "orchestrator-brainstorming", "Иван + cto", "5", "5", "3 (60%)", "2", "1", "1",
			"2", "Go после 2 правок", "2026-10-01 15:04", "Брейншторм"}},
		{"#5010", []string{"#5010", "orchestrator-brainstorming", "cto", "3", "3", "3 (100%)", "0", "0", "0",
			"0", "Go с 1-го раза", "2026-10-01 15:04", "Агент"}},
		{"#4950", []string{"#4950", "unknown", "—", "0", "0", "0 (—)", "0", "0", "0", "1", "ждёт Go (правок: 1)", "—", "Без Go"}},
		{"#4960", []string{"#4960", "unknown", "—", "0", "0", "0 (—)", "0", "0", "0", "0", "—", "—", "Без гейта"}},
	}
	for _, c := range checks {
		if got := lineWithPrefix(t, out, c.prefix); !reflect.DeepEqual(got, c.want) {
			t.Errorf("line %q:\n got %q\nwant %q\n%s", c.prefix, got, c.want, out)
		}
	}

	empty := renderBrainstormStats(brainstormStats{}, 4)
	for _, want := range []string{"последние 4 нед.", "ответов на вопросы шторма нет", "штормов нет"} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty output missing %q:\n%s", want, empty)
		}
	}
}
