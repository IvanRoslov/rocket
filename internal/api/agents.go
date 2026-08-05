package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentResponse is the JSON shape of a registered agent. Everything past the
// stored columns is derived: whether its tmux session is alive, how much is
// waiting in its inbox, and its open Q&A threads.
type agentResponse struct {
	ID           string `json:"id"`
	Description  string `json:"description"`
	Project      string `json:"project"`
	Dir          string `json:"dir"`
	Command      string `json:"command"`
	Enabled      bool   `json:"enabled"`
	SessionAlive bool   `json:"session_alive"`
	Unread       int    `json:"unread"`
	// OpenQuestions counts open Q&A threads; AwaitingUser counts the subset
	// whose turn it is for the human to speak.
	OpenQuestions int `json:"open_questions"`
	AwaitingUser  int `json:"awaiting_user"`
	// Milestones are the milestone tasks this agent holds (task #1023, spec v2):
	// the one place a human can see what the agent has taken on.
	Milestones []agentMilestoneRef `json:"milestones"`
	CreatedAt  int64               `json:"created_at"`
	UpdatedAt  int64               `json:"updated_at"`
}

// agentMilestoneRef is a milestone as listed on its holder's card.
type agentMilestoneRef struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// agentMilestones lists the milestones held by an agent, oldest first.
func agentMilestones(d Deps, agentID string) ([]agentMilestoneRef, error) {
	tasks, err := d.Store.ListTasks(store.TaskFilter{Milestones: true, AssignedRole: agentID})
	if err != nil {
		return nil, err
	}
	out := make([]agentMilestoneRef, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, agentMilestoneRef{ID: t.ID, Title: t.Title, Status: t.Status})
	}
	return out, nil
}

// inboxMessageResponse is the JSON shape of one inbox message.
type inboxMessageResponse struct {
	ID        int64  `json:"id"`
	From      string `json:"from"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	ReadAt    int64  `json:"read_at,omitempty"`
}

func toInboxMessageResponse(m store.InboxMessage) inboxMessageResponse {
	return inboxMessageResponse{
		ID:        m.ID,
		From:      m.From,
		Body:      m.Body,
		Status:    m.Status,
		CreatedAt: m.CreatedAt,
		ReadAt:    m.ReadAt,
	}
}

// agentSessionAlive reports whether the tmux session named after the agent is
// registered as live. Adoption of hand-made sessions happens in the daemon's
// watcher (internal/agentwatch); this is only the read side.
func agentSessionAlive(d Deps, id string) (bool, error) {
	sess, err := d.Store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if sess.Kind != session.AgentSessionKind {
		return false, nil
	}
	return sess.State == "spawning" || sess.State == "running", nil
}

// toAgentResponse renders an agent. counts is the roleQuestionCounts map,
// passed in so the list handler fetches it once instead of per agent.
func toAgentResponse(d Deps, a store.Agent, counts map[string]store.QuestionCounts) (agentResponse, error) {
	unread, err := d.Store.CountUnreadInbox(a.ID)
	if err != nil {
		return agentResponse{}, err
	}
	alive, err := agentSessionAlive(d, a.ID)
	if err != nil {
		return agentResponse{}, err
	}

	milestones, err := agentMilestones(d, a.ID)
	if err != nil {
		return agentResponse{}, err
	}

	qc := counts[a.ID]
	return agentResponse{
		ID:            a.ID,
		Description:   a.Description,
		Project:       a.ProjectID,
		Dir:           a.Dir,
		Command:       a.Command,
		Enabled:       a.Enabled,
		SessionAlive:  alive,
		Unread:        unread,
		OpenQuestions: qc.Open,
		AwaitingUser:  qc.AwaitingUser,
		Milestones:    milestones,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}, nil
}

// registerAgentRoutes wires the /v1/agents routes onto mux.
func registerAgentRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/agents", func(w http.ResponseWriter, r *http.Request) {
		handleListAgents(w, r, d)
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
	mux.HandleFunc("POST /v1/agents/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		handlePostAgentMessage(w, r, d)
	})
	mux.HandleFunc("GET /v1/agents/{id}/inbox", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentInbox(w, r, d)
	})
	mux.HandleFunc("POST /v1/agents/{id}/inbox/next", func(w http.ResponseWriter, r *http.Request) {
		handleAgentInboxNext(w, r, d)
	})
	mux.HandleFunc("GET /v1/agents/{id}/inbox/{msg}", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentInboxMessage(w, r, d)
	})
	mux.HandleFunc("POST /v1/agents/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		handleStartAgent(w, r, d)
	})
	mux.HandleFunc("POST /v1/agents/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		handleStopAgent(w, r, d)
	})

	registerAgentQuestionRoutes(mux, d)
}

// lookupAgent resolves the {id} path value, writing a 404 and returning
// ok == false when the agent doesn't exist.
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

func writeAgent(w http.ResponseWriter, d Deps, id string, status int) {
	a, err := d.Store.GetAgent(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	counts, err := roleQuestionCounts(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ar, err := toAgentResponse(d, a, counts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, status, ar)
}

func handleListAgents(w http.ResponseWriter, r *http.Request, d Deps) {
	agents, err := d.Store.ListAgents(r.URL.Query().Get("project"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	counts, err := roleQuestionCounts(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]agentResponse, 0, len(agents))
	for _, a := range agents {
		ar, err := toAgentResponse(d, a, counts)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out = append(out, ar)
	}
	writeJSON(w, http.StatusOK, out)
}

type postAgentRequest struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Project     string `json:"project"`
	Dir         string `json:"dir"`
	Command     string `json:"command"`
}

// handlePostAgent registers an agent: an id (which doubles as its tmux session
// name) plus optional description, project grouping and launcher fields.
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
	if req.Project != "" {
		if _, err := d.Store.GetProject(req.Project); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+req.Project)
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	a := store.Agent{
		ID:          req.ID,
		Description: req.Description,
		ProjectID:   req.Project,
		Dir:         req.Dir,
		Command:     req.Command,
		Enabled:     true,
	}
	if err := d.Store.AddAgent(a); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "agent_exists", "agent id already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeAgent(w, d, a.ID, http.StatusCreated)
}

func handleGetAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	writeAgent(w, d, a.ID, http.StatusOK)
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

	for field, target := range map[string]*string{
		"description": &a.Description,
		"dir":         &a.Dir,
		"command":     &a.Command,
	} {
		v, ok := raw[field]
		if !ok {
			continue
		}
		if err := json.Unmarshal(v, target); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid "+field)
			return
		}
	}
	if v, ok := raw["project"]; ok {
		var project string
		if err := json.Unmarshal(v, &project); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid project")
			return
		}
		if project != "" {
			if _, err := d.Store.GetProject(project); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+project)
					return
				}
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		}
		a.ProjectID = project
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
	writeAgent(w, d, a.ID, http.StatusOK)
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
	writeAgent(w, d, a.ID, http.StatusOK)
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

type postAgentMessageRequest struct {
	Body string `json:"body"`
	From string `json:"from"`
}

// handlePostAgentMessage is the dashboard's "send a message to this agent"
// endpoint. It takes the same live-or-inbox path as `rocket send <agent>`.
func handlePostAgentMessage(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	var req postAgentMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
		return
	}

	live, id, err := deliverToAgent(d, a.ID, req.From, req.Body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, agentDeliveryResult(a.ID, live, id))
}

// agentDeliveryResult is the response body every "message to an agent" path
// returns, so senders can tell an injected message from an inboxed one. id is
// the queued message's id when live, and the inbox row's id otherwise.
func agentDeliveryResult(agentID string, live bool, id int64) map[string]any {
	status := "inbox"
	if live {
		status = "queued"
	}
	return map[string]any{"id": id, "to": agentID, "status": status, "live": live}
}

func handleGetAgentInbox(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	status := r.URL.Query().Get("status")
	if status != "" && status != store.InboxUnread && status != store.InboxRead {
		writeErr(w, http.StatusBadRequest, "invalid_status", "status must be unread or read")
		return
	}

	msgs, err := d.Store.ListInboxMessages(a.ID, status, 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]inboxMessageResponse, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, toInboxMessageResponse(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAgentInboxNext hands out the agent's oldest unread message and marks
// it read — the pull half of the inbox. 204 means the inbox is drained.
func handleAgentInboxNext(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	m, found, err := d.Store.NextUnreadInboxMessage(a.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, toInboxMessageResponse(m))
}

// handleGetAgentInboxMessage reads one message without marking it read (peek).
func handleGetAgentInboxMessage(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("msg"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "message_not_found", "message not found")
		return
	}

	m, err := d.Store.GetInboxMessage(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "message_not_found", "message not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if m.AgentID != a.ID {
		writeErr(w, http.StatusNotFound, "message_not_found", "message not found")
		return
	}
	writeJSON(w, http.StatusOK, toInboxMessageResponse(m))
}

// handleStartAgent runs the thin launcher: a tmux session named after the
// agent, running its command in its directory. Rocket does not manage what
// happens inside.
func handleStartAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	if d.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "runtime_unavailable", "session manager is not running")
		return
	}

	sess, err := d.Manager.StartAgent(r.Context(), a)
	if err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": a.ID, "status": "running", "dir": sess.WorktreePath,
	})
}

// handleStopAgent kills the agent's tmux session; the registration stays.
func handleStopAgent(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}
	if d.Manager == nil {
		writeErr(w, http.StatusServiceUnavailable, "runtime_unavailable", "session manager is not running")
		return
	}

	if err := d.Manager.StopAgent(r.Context(), a.ID); err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID, "status": "stopped"})
}
