package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/store"
)

// A milestone (task #1023, spec v2) is a root task outside every project,
// taken by a persistent agent instead of started as a feature. This file holds
// the two verbs that move it between agents — take (the agent picks it up) and
// assign (the human hands it over or takes it back) — plus the mini-gate that
// keeps an empty milestone out of review.

// requireMilestone writes a 403 and returns false when the task is a regular
// project task: take/assign exist only for milestones, and saying so is more
// useful than a generic "forbidden".
func requireMilestone(w http.ResponseWriter, task store.Task) bool {
	if !task.Milestone {
		writeErr(w, http.StatusForbidden, "not_a_milestone",
			"only milestones are taken by agents: use rocket task start for a project task")
		return false
	}
	return true
}

// recordAssignment journals the change and publishes task.assigned, so both
// the human reading the task card and anything listening on the bus see who
// holds the milestone now. role == "" means it was released.
func recordAssignment(d Deps, task store.Task, caller *store.Session, role, verb string) {
	by := callerLabel(caller)
	d.Bus.Publish("task.assigned", task.SessionID, map[string]any{
		"task_id": task.ID, "agent_id": role, "by": by, "verb": verb,
	})

	var body string
	switch {
	case role == "":
		body = fmt.Sprintf("milestone unassigned (by %s)", by)
	case verb == "take":
		body = fmt.Sprintf("milestone taken by %s", role)
	default:
		body = fmt.Sprintf("milestone assigned to %s (by %s)", role, by)
	}
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "status",
		Body:   body,
		Author: callerAuthor(caller),
	}); err != nil {
		slog.Warn("api: milestone assignment log failed", "task_id", task.ID, "error", err)
	}
}

// writeTaskAfterAssignment re-reads the task and writes it, so the caller sees
// the stored assigned_role rather than what it hoped to store.
func writeTaskAfterAssignment(w http.ResponseWriter, d Deps, id int64) {
	updated, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(updated))
}

// handlePostTaskTake lets a persistent agent take an unassigned milestone.
// Only an agent session may call it: the point of the verb is that the agent
// picks the work up itself, visibly.
func handlePostTaskTake(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	task, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller == nil || caller.Kind != "agent" {
		writeErr(w, http.StatusForbidden, "agent_only",
			"only a persistent agent may take a milestone; the human assigns it with rocket task assign")
		return
	}
	if !requireMilestone(w, task) {
		return
	}

	switch task.AssignedRole {
	case caller.ID:
		// Already ours: taking twice is a no-op, not an error.
		writeJSON(w, http.StatusOK, toTaskResponse(task))
		return
	case "":
	default:
		writeErr(w, http.StatusConflict, "already_taken",
			"milestone is already taken by "+task.AssignedRole)
		return
	}

	if err := d.Store.SetTaskAssignedRole(task.ID, caller.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	recordAssignment(d, task, caller, caller.ID, "take")
	writeTaskAfterAssignment(w, d, task.ID)
}

type postTaskAssignRequest struct {
	AgentID string `json:"agent_id"`
	None    bool   `json:"none"`
}

// handlePostTaskAssign lets the human hand a milestone to an agent, or take it
// back with {none: true}. Agents cannot reassign each other's work: for them
// the only way in is take.
func handlePostTaskAssign(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	task, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "human_only",
			"only the human may assign a milestone; an agent takes one with rocket task take")
		return
	}
	if !requireMilestone(w, task) {
		return
	}

	var req postTaskAssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.None == (req.AgentID != "") {
		writeErr(w, http.StatusBadRequest, "bad_request", "pass exactly one of agent_id or none")
		return
	}

	role := req.AgentID
	if req.None {
		role = ""
	} else if _, err := d.Store.GetAgent(role); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "agent_not_found", "agent not found: "+role)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err := d.Store.SetTaskAssignedRole(task.ID, role); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	recordAssignment(d, task, caller, role, "assign")

	if role != "" {
		notice := fmt.Sprintf("[rocket] You have been assigned milestone #%d %q. Start with: rocket task show %d",
			task.ID, task.Title, task.ID)
		if _, _, err := deliverToAgent(d, role, "", notice); err != nil {
			slog.Warn("api: milestone assignment notice failed", "task_id", task.ID, "agent_id", role, "error", err)
		}
	}

	writeTaskAfterAssignment(w, d, task.ID)
}

// milestoneStatusAllowed enforces the milestone-specific rules on a status
// change, writing the refusal itself and returning false when it refuses:
//
//   - review needs something to review — at least one doc or one journal entry
//     from the holding agent. The auto-status entries take/assign/move write
//     don't count: they are bookkeeping, not work;
//   - done and cancelled are the human's acceptance, never the agent's.
func milestoneStatusAllowed(w http.ResponseWriter, d Deps, task store.Task, caller *store.Session, status string) bool {
	switch status {
	case "done", "cancelled":
		if caller != nil {
			writeErr(w, http.StatusForbidden, "human_only",
				"only the human accepts a milestone: ask for review with rocket task move "+
					strconv.FormatInt(task.ID, 10)+" review")
			return false
		}
	case "review":
		shown, err := milestoneHasWorkShown(d, task)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return false
		}
		if !shown {
			writeErr(w, http.StatusUnprocessableEntity, "milestone_empty",
				"nothing to review: put the result in a doc (rocket task doc put) or write up the work "+
					"in the journal (rocket task log) before asking for review")
			return false
		}
	}
	return true
}

// milestoneHasWorkShown reports whether the milestone carries anything a human
// could review: any doc, or any non-status journal entry written by the agent
// holding it.
func milestoneHasWorkShown(d Deps, task store.Task) (bool, error) {
	docs, err := d.Store.ListTaskDocs(task.ID, false)
	if err != nil {
		return false, err
	}
	if len(docs) > 0 {
		return true, nil
	}
	entries, err := d.Store.ListTaskLog(task.ID, "")
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Kind != "status" && e.Author != "" && (task.AssignedRole == "" || e.Author == task.AssignedRole) {
			return true, nil
		}
	}
	return false, nil
}
