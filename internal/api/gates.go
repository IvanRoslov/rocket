package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Storm exit gate (task #4901, spec v1 §2.3): the orchestrator requests a gate
// once spec (and plan) are written; the human decides go | changes. A gate is
// bound to the spec version current at request time, and a newer spec
// supersedes it — see supersedeGatesOnSpecPut.

// gateResponse is the JSON shape of a gate. Times are unix seconds like the
// rest of the API; plan_version and decided_at are null when absent.
type gateResponse struct {
	ID          int64  `json:"id"`
	TaskID      int64  `json:"task_id"`
	SpecVersion int64  `json:"spec_version"`
	PlanVersion *int64 `json:"plan_version"`
	Status      string `json:"status"`
	Comment     string `json:"comment"`
	DecidedBy   string `json:"decided_by"`
	RequestedBy string `json:"requested_by"`
	RequestedAt int64  `json:"requested_at"`
	DecidedAt   *int64 `json:"decided_at"`
}

func toGateResponse(g store.TaskGate) gateResponse {
	return gateResponse{
		ID:          g.ID,
		TaskID:      g.TaskID,
		SpecVersion: g.SpecVersion,
		PlanVersion: g.PlanVersion,
		Status:      g.Status,
		Comment:     g.Comment,
		DecidedBy:   g.DecidedBy,
		RequestedBy: g.RequestedBy,
		RequestedAt: g.RequestedAt,
		DecidedAt:   g.DecidedAt,
	}
}

func registerGateRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/tasks/{id}/gates", func(w http.ResponseWriter, r *http.Request) {
		handleRequestGate(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/gates", func(w http.ResponseWriter, r *http.Request) {
		handleListGates(w, r, d)
	})
	mux.HandleFunc("POST /v1/gates/{id}/decide", func(w http.ResponseWriter, r *http.Request) {
		handleDecideGate(w, r, d)
	})
}

// publishGate emits one task.gate_* event.
func publishGate(d Deps, typ, sessionID string, g store.TaskGate) {
	d.Bus.Publish(typ, sessionID, map[string]any{
		"task_id": g.TaskID, "gate_id": g.ID, "status": g.Status,
	})
}

func handleRequestGate(w http.ResponseWriter, r *http.Request, d Deps) {
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
	if !canWriteTask(caller, task, d.Store) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not request a gate on this task")
		return
	}

	gate, superseded, err := d.Store.RequestTaskGate(id, callerLabel(caller))
	if errors.Is(err, store.ErrNoSpec) {
		writeErr(w, http.StatusBadRequest, "no_spec", "task has no spec: put one with `rocket task doc put --kind spec` first")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, g := range superseded {
		publishGate(d, "task.gate_superseded", task.SessionID, g)
	}
	publishGate(d, "task.gate_requested", task.SessionID, gate)
	writeJSON(w, http.StatusCreated, toGateResponse(gate))
}

func handleListGates(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := getTaskOr404(w, d, id); !ok {
		return
	}
	gates, err := d.Store.ListTaskGates(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]gateResponse, len(gates))
	for i, g := range gates {
		out[i] = toGateResponse(g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"gates": out})
}

type decideGateRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment"`
}

// handleDecideGate records the human's go | changes. Only the human decides:
// any session caller, persistent agents included, gets 403.
func handleDecideGate(w http.ResponseWriter, r *http.Request, d Deps) {
	gateID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid gate id")
		return
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human decides a gate")
		return
	}

	var req decideGateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	req.Comment = strings.TrimSpace(req.Comment)
	switch req.Decision {
	case "go":
	case "changes":
		if req.Comment == "" {
			writeErr(w, http.StatusBadRequest, "comment_required", "needs-changes requires a comment")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "bad_request", `decision must be "go" or "changes"`)
		return
	}

	gate, err := d.Store.DecideTaskGate(gateID, req.Decision, req.Comment, callerLabel(caller))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", "gate not found")
		return
	case errors.Is(err, store.ErrGateNotPending):
		writeErr(w, http.StatusConflict, "gate_not_pending", "gate is "+gate.Status+", not pending: request a new gate")
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	task, err := d.Store.GetTask(gate.TaskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if gate.Status == "go" {
		if task.Status == "brainstorm" {
			if err := applyTaskStatusChange(d, task, caller, "in_progress"); err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		} else if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
			TaskID: task.ID,
			Kind:   "note",
			Body:   fmt.Sprintf("gate #%d: go по спеке v%d; статус задачи %s не тронут (не brainstorm)", gate.ID, gate.SpecVersion, task.Status),
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	publishGate(d, "task.gate_decided", task.SessionID, gate)

	// The decision stands whatever happens to the message: an orchestrator
	// that is gone simply misses it (deliverToSession skips dead sessions).
	if err := deliverToSession(d, task.SessionID, gateMessage(gate)); err != nil {
		slog.Warn("api: gate decision delivery failed", "gate_id", gate.ID, "session_id", task.SessionID, "err", err)
	}

	writeJSON(w, http.StatusOK, toGateResponse(gate))
}

// gateMessage is the text delivered to the task's orchestrator on a decision.
func gateMessage(g store.TaskGate) string {
	if g.Status == "changes" {
		return fmt.Sprintf("[rocket gate] Нужны правки по спеке v%d: %s", g.SpecVersion, g.Comment)
	}
	plan := "—"
	if g.PlanVersion != nil {
		plan = fmt.Sprintf("v%d", *g.PlanVersion)
	}
	return fmt.Sprintf("[rocket gate] Go по спеке v%d (план %s) — начинай реализацию.", g.SpecVersion, plan)
}

// supersedeGatesOnSpecPut invalidates the task's pending gate after a new spec
// version is written: the human must not approve a spec that has changed
// under them. Failures are logged, not returned — the doc write already
// succeeded and is what the caller asked for.
func supersedeGatesOnSpecPut(d Deps, task store.Task) {
	superseded, err := d.Store.SupersedePendingGates(task.ID)
	if err != nil {
		slog.Warn("api: supersede gates on spec put failed", "task_id", task.ID, "err", err)
		return
	}
	for _, g := range superseded {
		publishGate(d, "task.gate_superseded", task.SessionID, g)
	}
}
