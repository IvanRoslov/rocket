package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/modelpolicy"
	"github.com/IvanRoslov/rocket/internal/store"
)

// profileNameRe is the model profile name format (task #5026 spec).
var profileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// modelProfileResponse is a registry entry as GET/POST/PATCH
// /v1/model-profiles return it.
type modelProfileResponse struct {
	Name        string `json:"name"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Position    int    `json:"position"`
}

func toModelProfileResponse(p store.ModelProfile) modelProfileResponse {
	return modelProfileResponse{Name: p.Name, Agent: p.Agent, Model: p.Model, Effort: p.Effort,
		Description: p.Description, Enabled: p.Enabled, Position: p.Position}
}

// availableProfileResponse is a profile as offered to a launcher by GET
// /v1/model-profiles/available: no registry bookkeeping.
type availableProfileResponse struct {
	Name        string `json:"name"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// registerModelProfileRoutes wires the /v1/model-profiles routes onto mux.
func registerModelProfileRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
		handleListModelProfiles(w, d)
	})
	mux.HandleFunc("GET /v1/model-profiles/available", func(w http.ResponseWriter, r *http.Request) {
		handleAvailableModelProfiles(w, r, d)
	})
	mux.HandleFunc("POST /v1/model-profiles", func(w http.ResponseWriter, r *http.Request) {
		handlePostModelProfile(w, r, d)
	})
	mux.HandleFunc("PATCH /v1/model-profiles/{name}", func(w http.ResponseWriter, r *http.Request) {
		handlePatchModelProfile(w, r, d)
	})
	mux.HandleFunc("DELETE /v1/model-profiles/{name}", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteModelProfile(w, r, d)
	})
}

// requireHuman answers 403 human_only when the request comes from any agent
// session: the registry, the default profiles and task allowlists are the
// human's, or an orchestrator could widen its own choice. Returns true if it
// wrote a response.
func requireHuman(w http.ResponseWriter, r *http.Request, d Deps) bool {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return true
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "human_only",
			"only the human manages model profiles and allowlists; agent session "+caller.ID+" may not")
		return true
	}
	return false
}

func handleListModelProfiles(w http.ResponseWriter, d Deps) {
	ps, err := d.Store.ListModelProfiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]modelProfileResponse, 0, len(ps))
	for _, p := range ps {
		out = append(out, toModelProfileResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

// handleAvailableModelProfiles serves the profiles the caller may spawn a
// worker with: Allowed(feature task) for a session that belongs to a task
// (an orchestrator's own task; a worker's parent feature task), every
// enabled profile otherwise. "default" is what a spawn without --profile
// gets.
func handleAvailableModelProfiles(w http.ResponseWriter, r *http.Request, d Deps) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	var task *store.Task
	if caller != nil {
		if task, err = featureTaskOf(d.Store, caller.ID); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}
	in, err := policyInputs(d, task)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	allowed := modelpolicy.Allowed(in)
	out := make([]availableProfileResponse, 0, len(allowed))
	for _, p := range allowed {
		out = append(out, availableProfileResponse{Name: p.Name, Agent: p.Agent, Model: p.Model,
			Effort: p.Effort, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out, "default": modelpolicy.DefaultWorkerName(in)})
}

// featureTaskOf returns the feature task whose allowlist governs sessionID:
// the task it owns, or that task's parent when it owns a subtask. nil when
// the session owns no task (a persistent agent, a detached session).
func featureTaskOf(st *store.Store, sessionID string) (*store.Task, error) {
	t, err := st.GetTaskBySessionID(sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t.ParentID != 0 {
		parent, err := st.GetTask(t.ParentID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		t = parent
	}
	return &t, nil
}

// policyInputs loads the registry and default settings for modelpolicy;
// task (nil allowed) contributes its allowlist.
func policyInputs(d Deps, task *store.Task) (modelpolicy.Inputs, error) {
	ps, err := d.Store.ListModelProfiles()
	if err != nil {
		return modelpolicy.Inputs{}, err
	}
	in := modelpolicy.Inputs{Profiles: ps}
	if d.Cfg != nil {
		in.DefaultAgent = d.Cfg.DefaultAgent
	}
	if task != nil {
		in.TaskAllowed = task.AllowedProfiles
	}
	if in.DefaultWorker, err = optionalSetting(d.Store, store.SettingDefaultWorkerProfile); err != nil {
		return modelpolicy.Inputs{}, err
	}
	if in.DefaultOrch, err = optionalSetting(d.Store, store.SettingDefaultOrchestratorProfile); err != nil {
		return modelpolicy.Inputs{}, err
	}
	return in, nil
}

// optionalSetting reads key, mapping "unset" to "".
func optionalSetting(st *store.Store, key string) (string, error) {
	v, err := st.GetSetting(key)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	return v, err
}

// modelProfileRequest is the POST/PATCH body; nil fields are absent (PATCH
// leaves them unchanged, POST uses defaults).
type modelProfileRequest struct {
	Name        *string `json:"name"`
	Agent       *string `json:"agent"`
	Model       *string `json:"model"`
	Effort      *string `json:"effort"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
	Position    *int    `json:"position"`
}

// apply overlays the present fields of req onto p.
func (req modelProfileRequest) apply(p *store.ModelProfile) {
	if req.Agent != nil {
		p.Agent = *req.Agent
	}
	if req.Model != nil {
		p.Model = *req.Model
	}
	if req.Effort != nil {
		p.Effort = *req.Effort
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.Position != nil {
		p.Position = *req.Position
	}
}

// validateProfileAgent checks that p's agent is registered and its effort
// is one of that agent's levels. Returns true if it wrote a response.
func validateProfileAgent(w http.ResponseWriter, p store.ModelProfile) bool {
	ag, err := agent.Get(p.Agent)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "agent_unavailable", "unknown agent "+p.Agent)
		return true
	}
	if p.Effort != "" && !slices.Contains(ag.Efforts(), p.Effort) {
		writeErr(w, http.StatusBadRequest, "bad_effort",
			"effort "+p.Effort+" is not one of "+p.Agent+"'s: "+strings.Join(ag.Efforts(), ", "))
		return true
	}
	return false
}

func handlePostModelProfile(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	var req modelProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name == nil || !profileNameRe.MatchString(*req.Name) {
		writeErr(w, http.StatusBadRequest, "bad_request",
			"name must match "+profileNameRe.String())
		return
	}
	p := store.ModelProfile{Name: *req.Name, Enabled: true}
	if req.Position == nil {
		existing, err := d.Store.ListModelProfiles()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		for _, e := range existing {
			p.Position = max(p.Position, e.Position+1)
		}
	}
	req.apply(&p)
	if validateProfileAgent(w, p) {
		return
	}
	if err := d.Store.CreateModelProfile(p); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "profile_exists", "profile "+p.Name+" already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toModelProfileResponse(p))
}

func handlePatchModelProfile(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	name := r.PathValue("name")
	p, err := d.Store.GetModelProfile(name)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "profile_not_found", "no profile "+name)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	var req modelProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name != nil && *req.Name != name {
		writeErr(w, http.StatusBadRequest, "bad_request", "a profile cannot be renamed")
		return
	}
	req.apply(&p)
	if validateProfileAgent(w, p) {
		return
	}
	if err := d.Store.UpdateModelProfile(p); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toModelProfileResponse(p))
}

func handleDeleteModelProfile(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	name := r.PathValue("name")
	for _, key := range []string{store.SettingDefaultOrchestratorProfile, store.SettingDefaultWorkerProfile} {
		v, err := optionalSetting(d.Store, key)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if v == name {
			writeErr(w, http.StatusConflict, "profile_in_use",
				"profile "+name+" is the "+key+"; pick another default first")
			return
		}
	}
	if err := d.Store.DeleteModelProfile(name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "profile_not_found", "no profile "+name)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unknownProfiles returns the names in names that are not in the registry.
func unknownProfiles(st *store.Store, names []string) ([]string, error) {
	var missing []string
	for _, n := range names {
		if _, err := st.GetModelProfile(n); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			missing = append(missing, n)
		}
	}
	return missing, nil
}
