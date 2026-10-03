package usage

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func rate(v float64) *float64 { return &v }

func TestPeriodAggregatesBillableCostAndUniqueSessions(t *testing.T) {
	rows := []store.UsageRow{
		{SessionID: "s1", Agent: "claude", TaskID: 7, TaskTitle: "Feature", TaskStatus: "review", ProjectID: "p", Model: "a", Status: "ok", Tokens: store.UsageTokens{Input: 2, CacheWrite: 3, CacheRead: 7, Output: 5, Reasoning: 1}},
		{SessionID: "s1", Agent: "claude", TaskID: 7, TaskTitle: "Feature", TaskStatus: "review", ProjectID: "p", Model: "b", Status: "ok", Tokens: store.UsageTokens{Input: 4, CacheRead: 9}},
		{SessionID: "s2", Agent: "claude", Model: "a", Status: "ok", Tokens: store.UsageTokens{Input: 10, Output: 10}},
		{SessionID: "s3", Agent: "codex", TaskID: 7, TaskTitle: "Feature", TaskStatus: "review", ProjectID: "p", Model: "c", Status: "ok"},
		{SessionID: "s4", Agent: "claude", TaskID: 7, TaskTitle: "Feature", TaskStatus: "review", ProjectID: "p", Status: "missing"},
	}
	prices := []store.ModelPrice{
		{Model: "a", Input: rate(1), CacheWrite: rate(2), CacheRead: rate(.5), Output: rate(4)},
		{Model: "c"},
	}
	got := Period(rows, prices)
	if got.Totals.Sessions != 4 || got.Totals.Tokens != (Tokens{Input: 16, CacheWrite: 3, CacheRead: 16, Output: 15, Reasoning: 1, Billable: 34}) {
		t.Fatalf("totals = %+v", got.Totals)
	}
	if got.Totals.CostUSD == nil || math.Abs(*got.Totals.CostUSD-0.0000815) > 1e-12 || !got.Totals.CostPartial {
		t.Errorf("cost = %v, partial = %v", got.Totals.CostUSD, got.Totals.CostPartial)
	}
	if len(got.Models) != 3 || got.Models[0].Model != "a" || got.Models[0].Sessions != 2 || got.Models[0].Tokens.Billable != 30 || got.Models[1].Model != "b" || got.Models[1].CostUSD != nil || got.Models[2].Model != "c" || got.Models[2].CostUSD == nil || *got.Models[2].CostUSD != 0 {
		t.Errorf("models = %+v", got.Models)
	}
	if len(got.Tasks) != 2 || got.Tasks[0].TaskID != nil || got.Tasks[0].Title != "" || got.Tasks[0].Status != "" || got.Tasks[0].ProjectID != "" || got.Tasks[0].Sessions != 1 || got.Tasks[1].TaskID == nil || *got.Tasks[1].TaskID != 7 || got.Tasks[1].Sessions != 3 || got.Tasks[1].Tokens.Billable != 14 || !got.Tasks[1].CostPartial {
		t.Errorf("tasks = %+v", got.Tasks)
	}
}

func TestPeriodSeparatesSameModelByAgent(t *testing.T) {
	rows := []store.UsageRow{
		{SessionID: "a", Agent: "claude", Model: "shared", Status: "ok", Tokens: store.UsageTokens{Input: 1}},
		{SessionID: "b", Agent: "codex", Model: "shared", Status: "ok", Tokens: store.UsageTokens{Input: 2}},
	}
	got := Period(rows, []store.ModelPrice{{Model: "shared", Input: rate(1)}})
	if len(got.Models) != 2 || got.Models[0].Agent != "codex" || got.Models[0].Sessions != 1 || got.Models[1].Agent != "claude" || got.Models[1].Sessions != 1 {
		t.Fatalf("models = %+v", got.Models)
	}
}

func TestLargeFinitePriceDoesNotOverflowIntermediateProduct(t *testing.T) {
	got := modelCost(Tokens{Input: 2}, store.ModelPrice{Input: rate(1e308)})
	if got == nil || math.IsInf(*got, 0) || math.Abs(*got/1e302-2) > 1e-12 {
		t.Fatalf("large finite cost = %v, want 2e302", got)
	}
}

func TestOverflowingCostStillEncodesUsageJSON(t *testing.T) {
	rows := []store.UsageRow{
		{SessionID: "s1", Status: "ok", Model: "a", Tokens: store.UsageTokens{Input: 1_000_000}},
		{SessionID: "s1", Status: "ok", Model: "b", Tokens: store.UsageTokens{Input: 1_000_000}},
	}
	prices := []store.ModelPrice{{Model: "a", Input: rate(1e308)}, {Model: "b", Input: rate(1e308)}}
	for name, value := range map[string]any{
		"period": Period(rows, prices),
		"task":   Task(rows, prices, 7, nil),
	} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s usage cannot encode: %v", name, err)
		}
		var raw struct {
			Totals Totals `json:"totals"`
		}
		if err := json.Unmarshal(b, &raw); err != nil || raw.Totals.CostUSD != nil || !raw.Totals.CostPartial {
			t.Fatalf("%s overflowing total = %+v, err %v", name, raw.Totals, err)
		}
	}
}

func TestTaskIncludesUncollectedSessionsWithoutCountingThem(t *testing.T) {
	end := int64(160)
	rows := []store.UsageRow{
		{SessionID: "s1", Kind: "worker", Agent: "codex", Profile: "fast", Effort: "high", RepoID: "r", State: "done", Status: "ok", Final: true, StartedAt: 100, EndedAt: &end, PRNumber: 12, PRState: "merged", SubtaskID: 8, SubtaskTitle: "Ship", Model: "priced", Tokens: store.UsageTokens{Input: 10, CacheRead: 5, Output: 20}},
		{SessionID: "s1", Kind: "worker", Agent: "codex", Profile: "fast", Effort: "high", RepoID: "r", State: "done", Status: "ok", Final: true, StartedAt: 100, EndedAt: &end, PRNumber: 12, PRState: "merged", SubtaskID: 8, SubtaskTitle: "Ship", Model: "unpriced", Tokens: store.UsageTokens{Output: 1}},
		{SessionID: "s2", Kind: "worker", State: "errored", Status: "error", Error: "read denied", Final: true, StartedAt: 120, EndedAt: &end},
		{SessionID: "s3", Kind: "orchestrator", State: "running", StartedAt: 130},
		{SessionID: "s4", Kind: "worker", State: "killed", StartedAt: 140},
		{SessionID: "s5", Kind: "worker", State: "spawning", StartedAt: 150},
	}
	got := Task(rows, []store.ModelPrice{{Model: "priced", Input: rate(1), CacheRead: rate(1), Output: rate(1)}}, 7,
		func(repo string, number int) string { return "https://github.com/acme/" + repo + "/pull/12" })
	if got.TaskID != 7 || got.Totals.Sessions != 2 || got.Totals.Tokens.Billable != 31 || !got.Totals.CostPartial || len(got.Sessions) != 5 {
		t.Fatalf("task = %+v", got)
	}
	s1 := got.Sessions[0]
	if s1.Role != "worker" || s1.SubtaskID == nil || *s1.SubtaskID != 8 || s1.SubtaskTitle != "Ship" || s1.PRNumber == nil || *s1.PRNumber != 12 || s1.PRURL != "https://github.com/acme/r/pull/12" || s1.DurationS == nil || *s1.DurationS != 60 || s1.Tokens.Billable != 31 || s1.CostUSD != nil || len(s1.Models) != 2 {
		t.Errorf("s1 = %+v", s1)
	}
	if got.Sessions[1].Error != "read denied" || got.Sessions[1].CostUSD == nil || *got.Sessions[1].CostUSD != 0 {
		t.Errorf("error session = %+v", got.Sessions[1])
	}
	for i, want := range []string{"running", "pending", "running"} {
		s := got.Sessions[i+2]
		if s.Status != want || s.Final || s.EndedAt != nil || s.DurationS != nil || s.CostUSD != nil || len(s.Models) != 0 || s.Tokens.Billable != 0 || s.Error != "" {
			t.Errorf("uncollected session = %+v, want status %q", s, want)
		}
	}
}

func TestEmptyAggregatesEncodeArraysAndZeroCost(t *testing.T) {
	period := Period(nil, nil)
	task := Task(nil, nil, 7, nil)
	if period.Totals.Sessions != 0 || period.Totals.CostUSD == nil || *period.Totals.CostUSD != 0 || task.Totals.CostUSD == nil || *task.Totals.CostUSD != 0 {
		t.Fatalf("empty totals: period=%+v task=%+v", period, task)
	}
	for _, value := range []any{period, task} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(raw["models"], json.RawMessage("[]")) && !reflect.DeepEqual(raw["sessions"], json.RawMessage("[]")) {
			t.Fatalf("empty arrays missing: %s", b)
		}
	}
}
