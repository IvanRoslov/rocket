package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// inboxEventKinds are the event kinds a role's inbox accepts. Sources beyond
// `message` are produced by later layers (github poller, scheduler, Q&A
// threads); the set is validated here so a typo never lands in the queue.
var inboxEventKinds = map[string]bool{
	"message":         true,
	"issue_opened":    true,
	"issue_comment":   true,
	"task_update":     true,
	"snooze_expired":  true,
	"cron":            true,
	"question":        true,
	"terminal_opened": true,
}

// agentItemKinds are the dossier entry kinds.
var agentItemKinds = map[string]bool{"issue": true, "task": true, "ping": true}

// agentResponse is the JSON shape of a role.
type agentResponse struct {
	ID            string                    `json:"id"`
	Project       string                    `json:"project"`
	PromptPath    string                    `json:"prompt_path"`
	Prompt        string                    `json:"prompt,omitempty"`
	Subscriptions []store.AgentSubscription `json:"subscriptions"`
	Cron          string                    `json:"cron"`
	Agent         string                    `json:"agent"`
	Enabled       bool                      `json:"enabled"`
	InboxQueued   int                       `json:"inbox_queued"`
	Items         int                       `json:"items"`
	OpenQuestions int                       `json:"open_questions"`
	AwaitingUser  int                       `json:"awaiting_user"`
	CreatedAt     int64                     `json:"created_at"`
	UpdatedAt     int64                     `json:"updated_at"`
}

type inboxEventResponse struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	Status    string          `json:"status"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

type agentItemResponse struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	State       string `json:"state"`
	Note        string `json:"note"`
	TaskID      int64  `json:"task_id"`
	SnoozeUntil int64  `json:"snooze_until"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// roleFromSessionID returns the role a session belongs to. Role instances are
// named "<role>-run-<n>" (see docs/10-agents.md) — the session id is the only
// link between a run and its role, so no column duplicates it.
func roleFromSessionID(sessionID string) string {
	i := strings.LastIndex(sessionID, "-run-")
	if i <= 0 {
		return ""
	}
	return sessionID[:i]
}

// toAgentResponse renders a role. counts is the OpenAgentQuestionCounts map,
// passed in so the list handler fetches it once instead of per role.
func toAgentResponse(d Deps, a store.Agent, withPrompt bool, counts map[string]store.QuestionCounts) (agentResponse, error) {
	queued, err := d.Store.CountQueuedInboxEvents(a.ID)
	if err != nil {
		return agentResponse{}, err
	}
	items, err := d.Store.ListAgentItems(a.ID, "")
	if err != nil {
		return agentResponse{}, err
	}

	subs := a.Subscriptions
	if subs == nil {
		subs = []store.AgentSubscription{}
	}

	qc := counts[a.ID]

	resp := agentResponse{
		ID:            a.ID,
		Project:       a.ProjectID,
		PromptPath:    a.PromptPath,
		Subscriptions: subs,
		Cron:          a.Cron,
		Agent:         a.Agent,
		Enabled:       a.Enabled,
		InboxQueued:   queued,
		Items:         len(items),
		OpenQuestions: qc.Open,
		AwaitingUser:  qc.AwaitingUser,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
	if withPrompt {
		prompt, err := roles.ReadPrompt(a.PromptPath)
		if err != nil {
			return agentResponse{}, err
		}
		resp.Prompt = prompt
	}
	return resp, nil
}

// registerAgentRoutes wires the /v1/agents routes onto mux.
func registerAgentRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/agents", func(w http.ResponseWriter, r *http.Request) {
		agents, err := d.Store.ListAgents(r.URL.Query().Get("project"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		counts, err := d.Store.OpenAgentQuestionCounts()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out := make([]agentResponse, 0, len(agents))
		for _, a := range agents {
			ar, err := toAgentResponse(d, a, false, counts)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			out = append(out, ar)
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /v1/agents", func(w http.ResponseWriter, r *http.Request) {
		handlePostAgent(w, r, d)
	})
	mux.HandleFunc("GET /v1/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgent(w, r, d)
	})
	mux.HandleFunc("PATCH /v1/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		handlePatchAgent(w, r, d)
	})
	mux.HandleFunc("DELETE /v1/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteAgent(w, r, d)
	})
	mux.HandleFunc("POST /v1/agents/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		handleSetAgentEnabled(w, r, d, true)
	})
	mux.HandleFunc("POST /v1/agents/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		handleSetAgentEnabled(w, r, d, false)
	})
	mux.HandleFunc("POST /v1/agents/{id}/wake", func(w http.ResponseWriter, r *http.Request) {
		handleWakeAgent(w, r, d)
	})
	mux.HandleFunc("GET /v1/agents/{id}/inbox", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentInbox(w, r, d)
	})
	mux.HandleFunc("GET /v1/agents/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentItems(w, r, d)
	})
	mux.HandleFunc("PUT /v1/agents/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		handlePutAgentItem(w, r, d)
	})

	registerAgentQuestionRoutes(mux, d)
}

// lookupAgent resolves the {id} path value, writing a 404 and returning
// ok == false when the role doesn't exist.
func lookupAgent(w http.ResponseWriter, r *http.Request, d Deps) (store.Agent, bool) {
	a, err := d.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "agent_not_found", "agent not found")
			return store.Agent{}, false
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return store.Agent{}, false
	}
	return a, true
}

func writeAgent(w http.ResponseWriter, d Deps, id string, status int, withPrompt bool) {
	a, err := d.Store.GetAgent(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	counts, err := d.Store.OpenAgentQuestionCounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ar, err := toAgentResponse(d, a, withPrompt, counts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, status, ar)
}

type postAgentRequest struct {
	ID            string                    `json:"id"`
	Project       string                    `json:"project"`
	Prompt        string                    `json:"prompt"`
	Subscriptions []store.AgentSubscription `json:"subscriptions"`
	Cron          string                    `json:"cron"`
	Agent         string                    `json:"agent"`
}

func handlePostAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.ID == "" || !idPattern.MatchString(req.ID) {
		writeErr(w, http.StatusBadRequest, "invalid_id", "id must match ^[a-z0-9-]+$")
		return
	}
	if _, err := d.Store.GetProject(req.Project); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+req.Project)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if _, err := d.Store.GetAgent(req.ID); err == nil {
		writeErr(w, http.StatusConflict, "agent_exists", "agent id already exists")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	promptPath, err := roles.Ensure(d.Cfg.Home, req.ID, req.Prompt, true)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	underlying := req.Agent
	if underlying == "" {
		underlying = d.Cfg.DefaultAgent
	}

	a := store.Agent{
		ID:            req.ID,
		ProjectID:     req.Project,
		PromptPath:    promptPath,
		Subscriptions: req.Subscriptions,
		Cron:          req.Cron,
		Agent:         underlying,
		Enabled:       true,
	}
	if err := d.Store.AddAgent(a); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "agent_exists", "agent id already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeAgent(w, d, a.ID, http.StatusCreated, false)
}

func handleGetAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	writeAgent(w, d, a.ID, http.StatusOK, true)
}

func handlePatchAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if v, ok := raw["prompt"]; ok {
		var prompt string
		if err := json.Unmarshal(v, &prompt); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid prompt")
			return
		}
		promptPath, err := roles.Ensure(d.Cfg.Home, a.ID, prompt, true)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		a.PromptPath = promptPath
	}
	if v, ok := raw["subscriptions"]; ok {
		var subs []store.AgentSubscription
		if err := json.Unmarshal(v, &subs); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid subscriptions")
			return
		}
		a.Subscriptions = subs
	}
	if v, ok := raw["cron"]; ok {
		if err := json.Unmarshal(v, &a.Cron); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid cron")
			return
		}
	}
	if v, ok := raw["agent"]; ok {
		if err := json.Unmarshal(v, &a.Agent); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid agent")
			return
		}
	}
	if v, ok := raw["enabled"]; ok {
		if err := json.Unmarshal(v, &a.Enabled); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid enabled")
			return
		}
	}

	if err := d.Store.UpdateAgent(a); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeAgent(w, d, a.ID, http.StatusOK, true)
}

func handleSetAgentEnabled(w http.ResponseWriter, r *http.Request, d Deps, enabled bool) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	a.Enabled = enabled
	if err := d.Store.UpdateAgent(a); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeAgent(w, d, a.ID, http.StatusOK, false)
}

func handleDeleteAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	if err := d.Store.DeleteAgent(a.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type wakeAgentRequest struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// From names the sender (session id or user) for message events.
	From string `json:"from"`
	// Payload, when given, replaces the {text, from} payload built here.
	Payload json.RawMessage `json:"payload"`
}

// handleWakeAgent enqueues an inbox event. Spawning the role instance is the
// runtime layer's job (task #642): it watches the inbox and picks queued
// events up, so this endpoint stays enqueue-only.
func handleWakeAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	var req wakeAgentRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
	}

	kind := req.Kind
	if kind == "" {
		kind = "message"
	}
	if !inboxEventKinds[kind] {
		writeErr(w, http.StatusBadRequest, "invalid_kind", "unknown inbox event kind: "+kind)
		return
	}

	payload := string(req.Payload)
	if payload == "" {
		b, err := json.Marshal(map[string]string{"text": req.Text, "from": req.From})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		payload = string(b)
	}

	id, err := d.Store.EnqueueInboxEvent(store.AgentInboxEvent{
		RoleID:  a.ID,
		Kind:    kind,
		Payload: payload,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"event_id": id, "kind": kind})
}

func handleGetAgentInbox(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	events, err := d.Store.ListInboxEvents(a.ID, r.URL.Query().Get("status"), 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]inboxEventResponse, 0, len(events))
	for _, e := range events {
		payload := json.RawMessage(e.Payload)
		if !json.Valid(payload) {
			payload = json.RawMessage(`{}`)
		}
		out = append(out, inboxEventResponse{
			ID:        e.ID,
			Kind:      e.Kind,
			Payload:   payload,
			Status:    e.Status,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func handleGetAgentItems(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	items, err := d.Store.ListAgentItems(a.ID, r.URL.Query().Get("state"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]agentItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toAgentItemResponse(it))
	}
	writeJSON(w, http.StatusOK, out)
}

type putAgentItemRequest struct {
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	State       string `json:"state"`
	Note        string `json:"note"`
	TaskID      int64  `json:"task_id"`
	SnoozeUntil int64  `json:"snooze_until"`
}

func handlePutAgentItem(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	// A human caller (no session header) has full rights; a session caller
	// must be an instance of this very role.
	if caller != nil && (caller.Kind != "agent" || roleFromSessionID(caller.ID) != a.ID) {
		writeErr(w, http.StatusForbidden, "forbidden", "only instances of role "+a.ID+" may write its dossier")
		return
	}

	var req putAgentItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Ref) == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "ref is required")
		return
	}
	if !agentItemKinds[req.Kind] {
		writeErr(w, http.StatusBadRequest, "invalid_kind", "kind must be one of issue|task|ping")
		return
	}

	it, err := d.Store.UpsertAgentItem(store.AgentItem{
		RoleID:      a.ID,
		Kind:        req.Kind,
		ExternalRef: req.Ref,
		State:       req.State,
		Note:        req.Note,
		TaskID:      req.TaskID,
		SnoozeUntil: req.SnoozeUntil,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAgentItemResponse(it))
}

func toAgentItemResponse(it store.AgentItem) agentItemResponse {
	return agentItemResponse{
		ID:          it.ID,
		Kind:        it.Kind,
		Ref:         it.ExternalRef,
		State:       it.State,
		Note:        it.Note,
		TaskID:      it.TaskID,
		SnoozeUntil: it.SnoozeUntil,
		CreatedAt:   it.CreatedAt,
		UpdatedAt:   it.UpdatedAt,
	}
}
