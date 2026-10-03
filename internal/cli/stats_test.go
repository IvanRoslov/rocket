package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/usage"
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

type fakeStatsClient struct {
	getPath, putPath, deletePath string
	getReply                     any
	putBody                      map[string]any
}

func (f *fakeStatsClient) Get(path string, _, out any) error {
	f.getPath = path
	b, _ := json.Marshal(f.getReply)
	return json.Unmarshal(b, out)
}

func (f *fakeStatsClient) Put(path string, in, out any) error {
	f.putPath = path
	b, _ := json.Marshal(in)
	if err := json.Unmarshal(b, &f.putBody); err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal([]byte(`{"model":"vendor/gpt-6","input":2,"cache_write":3,"cache_read":null,"output":4,"updated_at":123}`), out)
	}
	return nil
}

func (f *fakeStatsClient) Delete(path string, _, _ any) error {
	f.deletePath = path
	return nil
}

func testStatsDial(f *fakeStatsClient) func() (statsClient, error) {
	return func() (statsClient, error) { return f, nil }
}

func TestStatsUsageCommandFiltersAndTable(t *testing.T) {
	f := &fakeStatsClient{getReply: statsUsageReply{
		From: "2026-10-01", To: "2026-10-03", Pending: 2,
		PeriodSummary: usage.PeriodSummary{
			Totals: usage.Totals{Sessions: 1, Tokens: usage.Tokens{Input: 5, CacheRead: 9, Billable: 5}, CostUSD: floatPtr(1.25), CostPartial: true},
			Models: []usage.ModelSummary{{Model: "m", Agent: "codex", Sessions: 1, Tokens: usage.Tokens{Input: 5, Billable: 5}}},
			Tasks:  []usage.TaskSummary{{Sessions: 1, Tokens: usage.Tokens{Input: 5, Billable: 5}}},
		},
	}}
	cmd := newStatsUsageCmd(testStatsDial(f))
	cmd.SetArgs([]string{"--from", "2026-10-01", "--to", "2026-10-03", "--project", "p/x"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.getPath != "/v1/stats/usage?from=2026-10-01&project=p%2Fx&to=2026-10-03" {
		t.Errorf("GET path=%q", f.getPath)
	}
	for _, want := range []string{"2026-10-01", "2026-10-03", "partial", "2", "m", "Без задачи", "—"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatsUsageEmptyTable(t *testing.T) {
	out := renderUsageStats(statsUsageReply{From: "2026-10-01", To: "2026-10-03", PeriodSummary: usage.PeriodSummary{Totals: usage.Totals{CostUSD: floatPtr(0)}}})
	if !strings.Contains(out, "нет расхода") {
		t.Fatalf("empty output=%q", out)
	}
}

func TestStatsTaskCommandAndSessionTable(t *testing.T) {
	f := &fakeStatsClient{getReply: usage.TaskUsageSummary{
		TaskID: 42, Totals: usage.Totals{Sessions: 1, Tokens: usage.Tokens{Billable: 10}, CostUSD: floatPtr(0.2)},
		Sessions: []usage.SessionSummary{{SessionID: "s-1", Role: "worker", SubtaskTitle: "Build", Status: "ok", State: "done", PRURL: "https://github.com/acme/r/pull/9", Models: []usage.SessionModel{{Model: "m", Tokens: usage.Tokens{Billable: 10}}}, Tokens: usage.Tokens{Billable: 10}}},
	}}
	cmd := newStatsTaskCmd(testStatsDial(f))
	cmd.SetArgs([]string{"42"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.getPath != "/v1/tasks/42/usage" {
		t.Errorf("GET path=%q", f.getPath)
	}
	for _, want := range []string{"#42", "s-1", "worker", "Build", "m", "10", "https://github.com/acme/r/pull/9"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatsPricesSetMergesOmittedRatesAndClearsNull(t *testing.T) {
	f := &fakeStatsClient{getReply: statsPricesReply{Prices: []statsPriceRow{{Model: "vendor/gpt-6", Input: floatPtr(1), CacheWrite: floatPtr(3), CacheRead: floatPtr(5), Output: floatPtr(4)}}}}
	cmd := newStatsPricesCmd(testStatsDial(f))
	cmd.SetArgs([]string{"set", "vendor/gpt-6", "--input", "2", "--cache-read", "null"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.getPath != "/v1/stats/prices" || f.putPath != "/v1/stats/prices/vendor%2Fgpt-6" {
		t.Errorf("paths GET=%q PUT=%q", f.getPath, f.putPath)
	}
	if f.putBody["input"] != float64(2) || f.putBody["cache_write"] != float64(3) || f.putBody["cache_read"] != nil || f.putBody["output"] != float64(4) {
		t.Errorf("merged body=%+v", f.putBody)
	}
}

func TestStatsPricesSetNewModelOmittedRatesAreNull(t *testing.T) {
	f := &fakeStatsClient{getReply: statsPricesReply{Prices: []statsPriceRow{}}}
	cmd := newStatsPricesCmd(testStatsDial(f))
	cmd.SetArgs([]string{"set", "new", "--output", "2"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.putBody["input"] != nil || f.putBody["cache_write"] != nil || f.putBody["cache_read"] != nil || f.putBody["output"] != float64(2) {
		t.Errorf("new body=%+v", f.putBody)
	}
}

func TestStatsPricesRemoveEscapesModel(t *testing.T) {
	f := &fakeStatsClient{}
	cmd := newStatsPricesCmd(testStatsDial(f))
	cmd.SetArgs([]string{"rm", "vendor/gpt-6?x"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.deletePath != "/v1/stats/prices/vendor%2Fgpt-6%3Fx" {
		t.Errorf("DELETE path=%q", f.deletePath)
	}
}

func TestStatsPricesListUnpricedAndPriced(t *testing.T) {
	out := renderPrices([]statsPriceRow{{Model: "a"}, {Model: "b", Input: floatPtr(1.5), Output: floatPtr(4)}})
	for _, want := range []string{"a", "b", "—", "1.5", "4"} {
		if !strings.Contains(out, want) {
			t.Errorf("prices missing %q:\n%s", want, out)
		}
	}
}

func TestStatsUsageTaskPricesArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"usage", "extra"}, {"task"}, {"task", "bad"}, {"task", "0"},
		{"prices", "set", "m"}, {"prices", "set", "m", "--input", "-1"},
		{"prices", "set", "m", "--output", "NaN"}, {"prices", "rm"}, {"prices", "rm", "m", "extra"},
	} {
		cmd := newStatsCmd()
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		var usageErr *usageError
		if err := cmd.Execute(); !errors.As(err, &usageErr) {
			t.Errorf("args=%v: want usageError, got %v", args, err)
		}
	}
}

func TestStatsUsageTaskPricesAreRegistered(t *testing.T) {
	seen := map[string]bool{}
	for _, cmd := range newStatsCmd().Commands() {
		seen[cmd.Name()] = true
	}
	for _, name := range []string{"usage", "task", "prices"} {
		if !seen[name] {
			t.Errorf("missing stats %s", name)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }

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

func TestStatsCollectSendsRequest(t *testing.T) {
	tests := []struct {
		name     string
		session  string
		all      bool
		retry    bool
		wantBody map[string]any
		wantOut  string
	}{
		{"session", "w1", false, false, map[string]any{"session_id": "w1"}, "w1"},
		{"all", "", true, false, map[string]any{"all": true}, "3"},
		{"all and missing", "", true, true, map[string]any{"all": true, "retry_missing": true}, "3"},
		{"missing only", "", false, true, map[string]any{"retry_missing": true}, "3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen, h := fakeDaemon(t, map[string]any{
				"POST /v1/stats/usage/collect": map[string]any{"queued": 3},
			})
			c := newUnixSocketTestServer(t, h)
			var out bytes.Buffer
			if err := runStatsCollect(c, &out, tt.session, tt.all, tt.retry); err != nil {
				t.Fatal(err)
			}
			if len(*seen) != 1 || (*seen)[0].Method != "POST" || (*seen)[0].Path != "/v1/stats/usage/collect" {
				t.Fatalf("requests = %+v", *seen)
			}
			if !reflect.DeepEqual((*seen)[0].Body, tt.wantBody) {
				t.Fatalf("body = %v, want %v", (*seen)[0].Body, tt.wantBody)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("output %q lacks %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestStatsCollectUsage(t *testing.T) {
	for _, args := range [][]string{
		{"collect"},
		{"collect", "--session", "w1", "--all"},
		{"collect", "extra"},
	} {
		cmd := newStatsCmd()
		cmd.SetArgs(args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		var usageErr *usageError
		if err := cmd.Execute(); !errors.As(err, &usageErr) {
			t.Errorf("%v: expected usageError, got %T: %v", args, err, err)
		}
	}
}
