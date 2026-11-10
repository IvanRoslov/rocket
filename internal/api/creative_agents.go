package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/IvanRoslov/rocket/internal/modelpolicy"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/toolcatalog"
)

type creativeSessionResponse struct {
	sessionResponse
	CreativeContext store.CreativeBinding `json:"creative_context"`
}
type creativeMessageResponse struct {
	messageResponse
	CreativeContext    store.CreativeBinding `json:"creative_context"`
	ArtifactVersionIDs []int64               `json:"artifact_version_ids"`
}
type creativeQuestionResponse struct {
	questionResponse
	CreativeContext    store.CreativeBinding `json:"creative_context"`
	ArtifactVersionIDs []int64               `json:"artifact_version_ids"`
}

func versionsOrEmpty(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
func scopeOnly(b store.CreativeBinding) store.CreativeBinding { b.ArtifactVersionIDs = nil; return b }
func registerCreativeAgentRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/creative/runs/{run}/workers/{session}/close", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) {
		handleCreativeClose(w, r, d, c)
	}))
	mux.HandleFunc("GET /v1/tools", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		entries, err := toolcatalog.Load(d.Cfg.Home)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, entries)
	}))
	mux.HandleFunc("POST /v1/creative/runs/{run}/sessions", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) {
		handleCreativeLaunch(w, r, d, c, false)
	}))
	mux.HandleFunc("POST /v1/creative/runs/{run}/workers", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) { handleCreativeLaunch(w, r, d, c, true) }))
	mux.HandleFunc("GET /v1/creative/runs/{run}/sessions", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) {
		b, ok := creativeQueryBinding(w, r)
		if !ok {
			return
		}
		if _, ok := creativeAgentAccess(w, d, c, b, false); !ok {
			return
		}
		ids, err := d.Store.ListCreativeSessionIDs(b.RunID)
		if err != nil {
			creativeError(w, err)
			return
		}
		out := []creativeSessionResponse{}
		for _, id := range ids {
			sess, err := d.Store.GetSession(id)
			if err != nil {
				creativeError(w, err)
				return
			}
			ctx, err := d.Store.GetCreativeSessionContext(id)
			if err != nil {
				creativeError(w, err)
				return
			}
			out = append(out, creativeSessionResponse{toSessionResponse(sess, inputStallThreshold(d.Cfg)), ctx})
		}
		writeJSON(w, 200, out)
	}))
	for _, kind := range []string{"messages", "questions"} {
		mux.HandleFunc("GET /v1/creative/runs/{run}/"+kind, creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) {
			handleCreativeDiscussionList(w, r, d, c, kind)
		}))
		mux.HandleFunc("POST /v1/creative/runs/{run}/"+kind, creativeRoute(d, func(w http.ResponseWriter, r *http.Request, c *store.Session) {
			handleCreativeDiscussionPost(w, r, d, c, kind)
		}))
	}
}
func creativeQueryBinding(w http.ResponseWriter, r *http.Request) (store.CreativeBinding, bool) {
	var b store.CreativeBinding
	var ok bool
	if b.RunID, ok = creativeID(w, r.PathValue("run"), false); !ok {
		return b, false
	}
	if b.StageID, ok = creativeID(w, r.URL.Query().Get("stage_id"), true); !ok {
		return b, false
	}
	if b.ItemID, ok = creativeID(w, r.URL.Query().Get("item_id"), true); !ok {
		return b, false
	}
	return b, true
}
func creativeAgentAccess(w http.ResponseWriter, d Deps, caller *store.Session, b store.CreativeBinding, scoped bool) (store.CreativeContext, bool) {
	c, err := d.Store.ValidateCreativeBinding(b)
	if err != nil {
		creativeError(w, err)
		return c, false
	}
	if caller == nil {
		return c, true
	}
	own, err := d.Store.GetCreativeSessionContext(caller.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		creativeError(w, err)
		return c, false
	}
	if err != nil || own.RunID != b.RunID || caller.ProjectID != c.ProjectID || (scoped && caller.Kind == "worker" && (own.StageID != b.StageID || own.ItemID != b.ItemID)) {
		writeErr(w, 403, "creative_forbidden", "caller must belong to this run and assigned discussion scope")
		return c, false
	}
	return c, true
}

type creativeLaunchRequest struct {
	AllowedProfiles *[]string `json:"allowed_profiles"`
	StageID         int64     `json:"stage_id"`
	ItemID          int64     `json:"item_id"`
	Repo            string    `json:"repo"`
	Name            string    `json:"name"`
	Prompt          string    `json:"prompt"`
	Profile         string    `json:"profile"`
	Agent           string    `json:"agent"`
}

func handleCreativeLaunch(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session, worker bool) {
	runID, ok := creativeID(w, r.PathValue("run"), false)
	if !ok {
		return
	}
	var in creativeLaunchRequest
	if !creativeBody(w, r, &in) {
		return
	}
	if in.AllowedProfiles != nil {
		if requireHuman(w, r, d) || !validateCreativeAllowedProfiles(w, d, *in.AllowedProfiles) {
			return
		}
	}
	b := store.CreativeBinding{RunID: runID, StageID: in.StageID, ItemID: in.ItemID}
	c, err := d.Store.ResolveCreativeContext(b.RunID, b.StageID, b.ItemID)
	if err != nil {
		creativeError(w, err)
		return
	}
	parent := ""
	if worker {
		if caller == nil || caller.ID != c.OrchestratorSessionID || !isSpawningOrchestrator(caller) {
			writeErr(w, 403, "creative_forbidden", "only the live run orchestrator may spawn workers")
			return
		}
		if _, ok := creativeAgentAccess(w, d, caller, b, false); !ok {
			return
		}
		parent = caller.ID
	} else if caller != nil {
		writeErr(w, 403, "creative_forbidden", "human launches the run orchestrator")
		return
	}
	inputs, err := policyInputs(d, nil)
	if err != nil {
		creativeError(w, err)
		return
	}
	var profile store.ModelProfile
	if worker {
		run, loadErr := d.Store.GetCreativeRun(runID)
		if loadErr != nil {
			creativeError(w, loadErr)
			return
		}
		inputs.TaskAllowed = run.AllowedProfiles
		profile, err = modelpolicy.ResolveWorker(inputs, in.Profile, in.Agent)
	} else {
		var found bool
		profile, found, err = modelpolicy.ResolveOrchestrator(inputs, in.Profile, in.Agent)
		if !found && err == nil {
			profile.Agent = in.Agent
		}
	}
	if writePolicyErr(w, err) {
		return
	}
	if !worker && in.AllowedProfiles != nil {
		if err := d.Store.SetCreativeRunAllowedProfiles(runID, *in.AllowedProfiles); err != nil {
			creativeError(w, err)
			return
		}
	}
	sess, err := d.Manager.SpawnCreative(r.Context(), session.CreativeSpawnReq{Context: b, ParentID: parent, Repo: in.Repo, Name: in.Name, Prompt: in.Prompt, AgentName: profile.Agent, Profile: session.LaunchProfile{Name: profile.Name, Model: profile.Model, Effort: profile.Effort}})
	if err != nil {
		var v *session.ValidationError
		if errors.As(err, &v) {
			writeManagerErr(w, err)
		} else {
			creativeError(w, err)
		}
		return
	}
	writeJSON(w, 201, creativeSessionResponse{toSessionResponse(sess, inputStallThreshold(d.Cfg)), b})
}
func creativeContextPrefix(b store.CreativeBinding) string {
	return fmt.Sprintf("[creative run=%d stage=%d item=%d artifact_versions=%v]", b.RunID, b.StageID, b.ItemID, versionsOrEmpty(b.ArtifactVersionIDs))
}
func wakeCreativeMessage(d Deps, id int64, to string) {
	if d.Bus != nil {
		d.Bus.Publish("message.queued", to, map[string]any{"id": id, "to": to})
	}
	if d.Queue != nil {
		d.Queue.Wake(to)
	}
}
func creativeMessageView(d Deps, id int64) (creativeMessageResponse, error) {
	m, err := d.Store.GetMessage(id)
	if err != nil {
		return creativeMessageResponse{}, err
	}
	b, err := d.Store.GetCreativeDiscussionContext("message", id)
	return creativeMessageResponse{toMessageResponse(m), scopeOnly(b), versionsOrEmpty(b.ArtifactVersionIDs)}, err
}
func creativeQuestionView(d Deps, caller *store.Session, id int64) (creativeQuestionResponse, error) {
	q, err := d.Store.GetQuestion(id)
	if err != nil {
		return creativeQuestionResponse{}, err
	}
	b, err := d.Store.GetCreativeDiscussionContext("question", id)
	if err != nil {
		return creativeQuestionResponse{}, err
	}
	resp, err := buildQuestionResponse(d, caller, q)
	resp.LocalRef = fmt.Sprintf("creative/Q%d", id)
	resp.Ordinal = int(id)
	return creativeQuestionResponse{resp, scopeOnly(b), versionsOrEmpty(b.ArtifactVersionIDs)}, err
}
func handleCreativeDiscussionList(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session, kind string) {
	b, ok := creativeQueryBinding(w, r)
	if !ok {
		return
	}
	if _, ok := creativeAgentAccess(w, d, caller, b, false); !ok {
		return
	}
	ids, err := d.Store.ListCreativeDiscussionIDs(strings.TrimSuffix(kind, "s"), b)
	if err != nil {
		creativeError(w, err)
		return
	}
	if kind == "messages" {
		out := []creativeMessageResponse{}
		for _, id := range ids {
			v, err := creativeMessageView(d, id)
			if err != nil {
				creativeError(w, err)
				return
			}
			out = append(out, v)
		}
		writeJSON(w, 200, out)
	} else {
		out := []creativeQuestionResponse{}
		for _, id := range ids {
			v, err := creativeQuestionView(d, caller, id)
			if err != nil {
				creativeError(w, err)
				return
			}
			out = append(out, v)
		}
		writeJSON(w, 200, out)
	}
}

type creativeDiscussionRequest struct {
	StageID            int64    `json:"stage_id"`
	ItemID             int64    `json:"item_id"`
	ArtifactVersionIDs []int64  `json:"artifact_version_ids"`
	Body               string   `json:"body"`
	Title              string   `json:"title"`
	Options            []string `json:"options"`
	To                 string   `json:"to"`
}

func handleCreativeDiscussionPost(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session, kind string) {
	if kind == "questions" {
		runID, ok := creativeID(w, r.PathValue("run"), false)
		if !ok {
			return
		}
		if !creativeFeedAccess(w, d, caller, runID, false) {
			return
		}
		hint := fmt.Sprintf("use rocket creative agent feed post %d --body 'Your message'; paid proposals use rocket creative agent feed propose %d --body 'Exact calls and cost'", runID, runID)
		if caller != nil && caller.Kind == "worker" {
			hint = fmt.Sprintf("message the orchestrator with rocket creative agent send %d --body 'Your question'", runID)
		}
		writeErr(w, 409, "creative_questions_retired", "new Creative questions are retired; "+hint)
		return
	}

	runID, ok := creativeID(w, r.PathValue("run"), false)
	if !ok {
		return
	}
	var in creativeDiscussionRequest
	if !creativeBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Body) == "" {
		writeErr(w, 400, "empty_body", "body must not be empty")
		return
	}
	b := store.CreativeBinding{RunID: runID, StageID: in.StageID, ItemID: in.ItemID, ArtifactVersionIDs: in.ArtifactVersionIDs}
	c, ok := creativeAgentAccess(w, d, caller, b, true)
	if !ok {
		return
	}
	if kind == "messages" {
		to := in.To
		if to == "" {
			to = c.OrchestratorSessionID
		}
		if caller != nil && to == caller.ID {
			writeErr(w, http.StatusBadRequest, "self_send", "cannot send a Creative message to yourself; use --to <recipient-session> (orchestrators must name the worker); list recipients with rocket creative agent sessions RUN")
			return
		}
		target, err := d.Store.GetSession(to)
		if err != nil {
			creativeError(w, err)
			return
		}
		bound, err := d.Store.GetCreativeSessionContext(to)
		if err != nil {
			creativeError(w, err)
			return
		}
		if bound.RunID != b.RunID {
			writeErr(w, 403, "creative_forbidden", "recipient belongs to another run")
			return
		}
		if target.State != "running" && target.State != "spawning" {
			writeErr(w, 409, "recipient_terminal", "recipient is not active; restore the session first")
			return
		}
		id, err := d.Store.AddCreativeMessage(store.Message{FromSession: callerAuthor(caller), ToSession: to, Body: creativeContextPrefix(b) + "\n" + rewriteAttachmentLinks(d, in.Body)}, b)
		if err != nil {
			creativeError(w, err)
			return
		}
		wakeCreativeMessage(d, id, to)
		resp, err := creativeMessageView(d, id)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 201, resp)
		return
	}
}

func creativeQuestionFanOut(d Deps, id int64, b store.CreativeBinding, author, body string) error {

	context, err := d.Store.ResolveCreativeContext(b.RunID, b.StageID, b.ItemID)
	if err != nil {
		return err
	}
	if context.OrchestratorSessionID != "" {
		if err := d.Store.AddParticipants(id, context.OrchestratorSessionID); err != nil {
			return err
		}
	}
	people, err := d.Store.ListParticipants(id)
	if err != nil {
		return err
	}
	for _, to := range people {
		if to == author || store.IsHuman(to) {
			continue
		}
		target, err := d.Store.GetSession(to)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if target.State != "running" && target.State != "spawning" {
			continue
		}
		msg, err := d.Store.AddCreativeMessage(store.Message{FromSession: author, ToSession: to, Body: fmt.Sprintf("%s [creative/Q%d from %s; reply with rocket creative agent reply %d]\n%s", creativeContextPrefix(b), id, author, id, body)}, b)
		if err != nil {
			return err
		}
		wakeCreativeMessage(d, msg, to)
	}
	if d.Bus != nil {
		d.Bus.Publish("creative.question_changed", "", map[string]any{"run_id": b.RunID, "question_id": id})
	}
	return nil
}
