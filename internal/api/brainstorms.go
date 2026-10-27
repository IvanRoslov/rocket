// The brainstorms list (spec 2026-10-27, §2.1): every task whose storm is
// waiting on the human — an open brainstorm question whose turn is the
// human's, or a pending Go gate. Read-only; answering and deciding stay on
// the task page.
package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/IvanRoslov/rocket/internal/store"
)

// hiddenBrainstormStatuses are task statuses past the storm: the work moved
// on, so a leftover question or gate is no longer the human's to act on. Any
// other status — including ones added later — stays visible.
var hiddenBrainstormStatuses = map[string]bool{"review": true, "done": true, "cancelled": true}

type pendingGate struct {
	ID          int64 `json:"id"`
	SpecVersion int64 `json:"spec_version"`
	RequestedAt int64 `json:"requested_at"`
}

// brainstormEntry is one task waiting on the human. WaitingSince is the
// oldest moment the task started waiting, across its questions and its gate.
type brainstormEntry struct {
	TaskID           int64        `json:"task_id"`
	ProjectID        string       `json:"project_id,omitempty"`
	TaskTitle        string       `json:"task_title"`
	TaskStatus       string       `json:"task_status"`
	QuestionsWaiting int          `json:"questions_waiting"`
	QuestionsStale   bool         `json:"questions_stale"`
	PendingGate      *pendingGate `json:"pending_gate"`
	WaitingSince     int64        `json:"waiting_since"`
}

func registerBrainstormRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/brainstorms", func(w http.ResponseWriter, r *http.Request) {
		handleGetBrainstorms(w, r, d)
	})
}

// sortBrainstorms puts tasks with a stale question first, then the longest
// waiting, then by task id so the order is stable.
func sortBrainstorms(es []brainstormEntry) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.QuestionsStale != b.QuestionsStale {
			return a.QuestionsStale
		}
		if a.WaitingSince != b.WaitingSince {
			return a.WaitingSince < b.WaitingSince
		}
		return a.TaskID < b.TaskID
	})
}

// handleGetBrainstorms serves GET /v1/brainstorms. It is the human's list: a
// session caller gets 403. Permission is not per-thread here — the human reads
// everything, so a task with a gate and no threads at all is still listed.
func handleGetBrainstorms(w http.ResponseWriter, r *http.Request, d Deps) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "forbidden", "brainstorms are the human's list")
		return
	}

	threads, err := d.Store.ListThreads(false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	gates, err := d.Store.ListPendingGates()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	entries := map[int64]*brainstormEntry{}
	// skipped remembers tasks that are gone or hidden, so each is read once.
	skipped := map[int64]bool{}
	entryFor := func(taskID int64) (*brainstormEntry, bool, error) {
		if e, ok := entries[taskID]; ok {
			return e, true, nil
		}
		if skipped[taskID] {
			return nil, false, nil
		}
		task, err := d.Store.GetTask(taskID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && hiddenBrainstormStatuses[task.Status]) {
			skipped[taskID] = true
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		e := &brainstormEntry{
			TaskID:     task.ID,
			ProjectID:  task.ProjectID,
			TaskTitle:  task.Title,
			TaskStatus: task.Status,
		}
		entries[taskID] = e
		return e, true, nil
	}
	// waitingSince lowers the entry's WaitingSince; zero means "not set yet".
	waitingSince := func(e *brainstormEntry, at int64) {
		if e.WaitingSince == 0 || at < e.WaitingSince {
			e.WaitingSince = at
		}
	}

	for _, th := range threads {
		q := th.Question
		if q.Type != store.QuestionTypeBrainstorm || q.TaskID == 0 || !hasHuman(th.Attention) {
			continue
		}
		e, ok, err := entryFor(q.TaskID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if !ok {
			continue
		}
		updatedAt := q.AskedAt
		if th.LastMessage != nil {
			updatedAt = th.LastMessage.CreatedAt
		}
		e.QuestionsWaiting++
		e.QuestionsStale = e.QuestionsStale || threadStale(d, q, th.LastMessage, th.Attention)
		waitingSince(e, updatedAt)
	}
	for _, g := range gates {
		e, ok, err := entryFor(g.TaskID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if !ok {
			continue
		}
		e.PendingGate = &pendingGate{ID: g.ID, SpecVersion: g.SpecVersion, RequestedAt: g.RequestedAt}
		waitingSince(e, g.RequestedAt)
	}

	out := make([]brainstormEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, *e)
	}
	sortBrainstorms(out)
	writeJSON(w, http.StatusOK, map[string]any{"brainstorms": out})
}

func hasHuman(ids []string) bool {
	for _, id := range ids {
		if store.IsHuman(id) {
			return true
		}
	}
	return false
}
