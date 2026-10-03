// Package usage turns collected per-session model rows into read-time summaries.
package usage

import (
	"math"
	"sort"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Tokens is the public token breakdown. Reasoning is included in output;
// billable deliberately excludes cache reads.
type Tokens struct {
	Input      int64 `json:"input"`
	CacheWrite int64 `json:"cache_write"`
	CacheRead  int64 `json:"cache_read"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
	Billable   int64 `json:"billable"`
}

func tokens(v store.UsageTokens) Tokens {
	return Tokens{Input: v.Input, CacheWrite: v.CacheWrite, CacheRead: v.CacheRead,
		Output: v.Output, Reasoning: v.Reasoning, Billable: v.Input + v.CacheWrite + v.Output}
}

func (t *Tokens) add(other Tokens) {
	t.Input += other.Input
	t.CacheWrite += other.CacheWrite
	t.CacheRead += other.CacheRead
	t.Output += other.Output
	t.Reasoning += other.Reasoning
	t.Billable += other.Billable
}

type Totals struct {
	Sessions    int      `json:"sessions"`
	Tokens      Tokens   `json:"tokens"`
	CostUSD     *float64 `json:"cost_usd"`
	CostPartial bool     `json:"cost_partial"`
}

type ModelSummary struct {
	Model    string   `json:"model"`
	Agent    string   `json:"agent"`
	Sessions int      `json:"sessions"`
	Tokens   Tokens   `json:"tokens"`
	CostUSD  *float64 `json:"cost_usd"`
}

type TaskSummary struct {
	TaskID      *int64   `json:"task_id"`
	Title       string   `json:"title"`
	ProjectID   string   `json:"project_id"`
	Status      string   `json:"status"`
	Sessions    int      `json:"sessions"`
	Tokens      Tokens   `json:"tokens"`
	CostUSD     *float64 `json:"cost_usd"`
	CostPartial bool     `json:"cost_partial"`
}

type SessionModel struct {
	Model   string   `json:"model"`
	Tokens  Tokens   `json:"tokens"`
	CostUSD *float64 `json:"cost_usd"`
}

type SessionSummary struct {
	SessionID    string         `json:"session_id"`
	Role         string         `json:"role"`
	SubtaskID    *int64         `json:"subtask_id"`
	SubtaskTitle string         `json:"subtask_title"`
	Agent        string         `json:"agent"`
	Profile      string         `json:"profile"`
	Effort       string         `json:"effort"`
	RepoID       string         `json:"repo_id"`
	PRNumber     *int           `json:"pr_number"`
	PRURL        string         `json:"pr_url"`
	PRState      string         `json:"pr_state"`
	State        string         `json:"state"`
	Status       string         `json:"status"`
	Final        bool           `json:"final"`
	Error        string         `json:"error"`
	StartedAt    int64          `json:"started_at"`
	EndedAt      *int64         `json:"ended_at"`
	DurationS    *int64         `json:"duration_s"`
	Models       []SessionModel `json:"models"`
	Tokens       Tokens         `json:"tokens"`
	CostUSD      *float64       `json:"cost_usd"`
	CostPartial  bool           `json:"cost_partial"`
}

type PeriodSummary struct {
	Totals Totals         `json:"totals"`
	Models []ModelSummary `json:"models"`
	Tasks  []TaskSummary  `json:"tasks"`
}

type TaskUsageSummary struct {
	TaskID   int64            `json:"task_id"`
	Totals   Totals           `json:"totals"`
	Sessions []SessionSummary `json:"sessions"`
}

func zeroCost() *float64 {
	v := 0.0
	return &v
}

func priceMap(prices []store.ModelPrice) map[string]store.ModelPrice {
	out := make(map[string]store.ModelPrice, len(prices))
	for _, p := range prices {
		out[p.Model] = p
	}
	return out
}

type modelKey struct{ model, agent string }

func sortedModelNames(models map[string]Tokens) []string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// modelCost returns nil when a nonzero category lacks a rate or the
// calculated amount cannot be represented as a finite JSON number.
func modelCost(t Tokens, p store.ModelPrice) *float64 {
	var cost float64
	for _, part := range []struct {
		n int64
		p *float64
	}{{t.Input, p.Input}, {t.CacheWrite, p.CacheWrite}, {t.CacheRead, p.CacheRead}, {t.Output, p.Output}} {
		if part.n == 0 {
			continue
		}
		if part.p == nil {
			return nil
		}
		cost += float64(part.n) / 1_000_000 * *part.p
		if math.IsInf(cost, 0) || math.IsNaN(cost) {
			return nil
		}
	}
	return &cost
}

func addFiniteCost(sum **float64, cost *float64) {
	if *sum == nil || cost == nil {
		*sum = nil
		return
	}
	next := **sum + *cost
	if math.IsInf(next, 0) || math.IsNaN(next) {
		*sum = nil
		return
	}
	**sum = next
}

func addPartialCost(sum **float64, partial *bool, cost *float64) {
	if cost == nil {
		*partial = true
		return
	}
	// A mathematically valid sum can exceed float64. Return null/partial
	// instead of emitting nonfinite JSON (which encoding/json rejects).
	if *sum != nil {
		addFiniteCost(sum, cost)
		if *sum == nil {
			*partial = true
		}
	}
}

func addCost(total *Totals, cost *float64) {
	addPartialCost(&total.CostUSD, &total.CostPartial, cost)
}

func isCollected(row store.UsageRow) bool {
	return row.Status != ""
}

// Period groups collected rows by actual model and root task. Session counts
// are distinct within each group even when one session used several models.
func Period(rows []store.UsageRow, prices []store.ModelPrice) PeriodSummary {
	out := PeriodSummary{Totals: Totals{CostUSD: zeroCost()}, Models: []ModelSummary{}, Tasks: []TaskSummary{}}
	byPrice := priceMap(prices)
	models := make(map[modelKey]*ModelSummary)
	tasks := make(map[int64]*TaskSummary)
	taskModels := make(map[int64]map[string]Tokens)
	allSessions := make(map[string]bool)
	modelSessions := make(map[modelKey]map[string]bool)
	taskSessions := make(map[int64]map[string]bool)
	for _, row := range rows {
		if !isCollected(row) {
			continue
		}
		if !allSessions[row.SessionID] {
			out.Totals.Sessions++
			allSessions[row.SessionID] = true
		}
		task := tasks[row.TaskID]
		if task == nil {
			task = &TaskSummary{CostUSD: zeroCost()}
			if row.TaskID != 0 {
				id := row.TaskID
				task.TaskID = &id
				task.Title, task.ProjectID, task.Status = row.TaskTitle, row.ProjectID, row.TaskStatus
			}
			tasks[row.TaskID] = task
			taskSessions[row.TaskID] = make(map[string]bool)
		}
		if !taskSessions[row.TaskID][row.SessionID] {
			task.Sessions++
			taskSessions[row.TaskID][row.SessionID] = true
		}
		if row.Model == "" {
			continue
		}
		t := tokens(row.Tokens)
		out.Totals.Tokens.add(t)
		task.Tokens.add(t)
		if taskModels[row.TaskID] == nil {
			taskModels[row.TaskID] = make(map[string]Tokens)
		}
		taskModelTokens := taskModels[row.TaskID][row.Model]
		taskModelTokens.add(t)
		taskModels[row.TaskID][row.Model] = taskModelTokens
		key := modelKey{row.Model, row.Agent}
		model := models[key]
		if model == nil {
			model = &ModelSummary{Model: row.Model, Agent: row.Agent, CostUSD: zeroCost()}
			models[key] = model
			modelSessions[key] = make(map[string]bool)
		}
		model.Tokens.add(t)
		if !modelSessions[key][row.SessionID] {
			model.Sessions++
			modelSessions[key][row.SessionID] = true
		}
	}
	for _, model := range models {
		model.CostUSD = modelCost(model.Tokens, byPrice[model.Model])
		out.Models = append(out.Models, *model)
	}
	sort.Slice(out.Models, func(i, j int) bool {
		a, b := out.Models[i], out.Models[j]
		if a.Tokens.Billable != b.Tokens.Billable {
			return a.Tokens.Billable > b.Tokens.Billable
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Agent < b.Agent
	})
	for _, model := range out.Models {
		addCost(&out.Totals, model.CostUSD)
	}
	for id, task := range tasks {
		for _, model := range sortedModelNames(taskModels[id]) {
			addPartialCost(&task.CostUSD, &task.CostPartial, modelCost(taskModels[id][model], byPrice[model]))
		}
		out.Tasks = append(out.Tasks, *task)
	}
	sort.Slice(out.Tasks, func(i, j int) bool {
		a, b := out.Tasks[i], out.Tasks[j]
		if a.Tokens.Billable != b.Tokens.Billable {
			return a.Tokens.Billable > b.Tokens.Billable
		}
		if a.TaskID == nil {
			return false
		}
		if b.TaskID == nil {
			return true
		}
		return *a.TaskID < *b.TaskID
	})
	return out
}

// Task retains every linked session, including live and pending sessions
// without a collection row. Totals include only collected sessions.
func Task(rows []store.UsageRow, prices []store.ModelPrice, taskID int64, prURLForRepo func(string, int) string) TaskUsageSummary {
	out := TaskUsageSummary{TaskID: taskID, Totals: Totals{CostUSD: zeroCost()}, Sessions: []SessionSummary{}}
	byPrice := priceMap(prices)
	index := make(map[string]int)
	totalModels := make(map[string]Tokens)
	for _, row := range rows {
		i, exists := index[row.SessionID]
		if !exists {
			s := SessionSummary{
				SessionID: row.SessionID, Role: row.Kind, SubtaskTitle: row.SubtaskTitle,
				Agent: row.Agent, Profile: row.Profile, Effort: row.Effort, RepoID: row.RepoID,
				PRState: row.PRState, State: row.State, Status: row.Status, Final: row.Final,
				Error: row.Error, StartedAt: row.StartedAt, EndedAt: row.EndedAt,
				Models: []SessionModel{}, Tokens: Tokens{}, CostUSD: zeroCost(),
			}
			if row.SubtaskID != 0 {
				id := row.SubtaskID
				s.SubtaskID = &id
			}
			if row.PRNumber > 0 {
				number := row.PRNumber
				s.PRNumber = &number
				if prURLForRepo != nil {
					s.PRURL = prURLForRepo(row.RepoID, number)
				}
			}
			if row.EndedAt != nil {
				duration := *row.EndedAt - row.StartedAt
				if duration < 0 {
					duration = 0
				}
				s.DurationS = &duration
			}
			if !isCollected(row) {
				if row.State == "running" || row.State == "spawning" {
					s.Status = "running"
				} else {
					s.Status = "pending"
				}
				s.CostUSD = nil
			} else {
				out.Totals.Sessions++
			}
			out.Sessions = append(out.Sessions, s)
			i = len(out.Sessions) - 1
			index[row.SessionID] = i
		}
		if row.Model == "" {
			continue
		}
		s := &out.Sessions[i]
		t := tokens(row.Tokens)
		cost := modelCost(t, byPrice[row.Model])
		s.Models = append(s.Models, SessionModel{Model: row.Model, Tokens: t, CostUSD: cost})
		s.Tokens.add(t)
		out.Totals.Tokens.add(t)
		modelTokens := totalModels[row.Model]
		modelTokens.add(t)
		totalModels[row.Model] = modelTokens
		addPartialCost(&s.CostUSD, &s.CostPartial, cost)
	}
	for _, model := range sortedModelNames(totalModels) {
		addCost(&out.Totals, modelCost(totalModels[model], byPrice[model]))
	}
	sort.Slice(out.Sessions, func(i, j int) bool {
		a, b := out.Sessions[i], out.Sessions[j]
		if a.StartedAt != b.StartedAt {
			return a.StartedAt < b.StartedAt
		}
		return a.SessionID < b.SessionID
	})
	return out
}
