package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/store"
)

func creativeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "creative_not_found", err.Error())
	case errors.Is(err, store.ErrCreativeInvalid):
		writeErr(w, 400, "creative_invalid", err.Error())
	case errors.Is(err, store.ErrCreativeConflict):
		writeErr(w, 409, "creative_conflict", err.Error())
	case errors.Is(err, store.ErrCreativeForbidden):
		writeErr(w, 403, "creative_forbidden", err.Error())
	default:
		writeErr(w, 500, "internal_error", err.Error())
	}
}
func creativeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, 400, "bad_request", "body must contain one JSON value")
		return false
	}
	return true
}
func creativeID(w http.ResponseWriter, value string, optional bool) (int64, bool) {
	if value == "" && optional {
		return 0, true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "creative_invalid", "IDs must be positive integers")
		return 0, false
	}
	return id, true
}
func creativeProjectWrite(w http.ResponseWriter, caller *store.Session, project string) bool {
	if caller != nil && caller.ProjectID != project {
		writeErr(w, 403, "creative_forbidden", "caller must belong to the creative project")
		return false
	}
	return true
}

// creativeRoute resolves identity even for reads. TCP device authentication and
// Unix socket trust remain the server's existing transport responsibility.
func creativeRoute(d Deps, fn func(http.ResponseWriter, *http.Request, *store.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, err := callerSession(r, d.Store)
		if writeCallerErr(w, err) {
			return
		}
		fn(w, r, caller)
	}
}
func registerCreativeRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/creative/pipelines", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		out, err := d.Store.ListCreativePipelines(r.URL.Query().Get("project_id"))
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/creative/pipelines", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		var in store.CreativePipelineInput
		if !creativeBody(w, r, &in) || !creativeProjectWrite(w, caller, in.ProjectID) {
			return
		}
		out, err := d.Store.CreateCreativePipeline(in)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 201, out)
	}))
	mux.HandleFunc("GET /v1/creative/pipelines/{id}", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		id, ok := creativeID(w, r.PathValue("id"), false)
		if !ok {
			return
		}
		out, err := d.Store.GetCreativePipeline(id)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("PUT /v1/creative/pipelines/{id}", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		id, ok := creativeID(w, r.PathValue("id"), false)
		if !ok {
			return
		}
		existing, err := d.Store.GetCreativePipeline(id)
		if err != nil {
			creativeError(w, err)
			return
		}
		if !creativeProjectWrite(w, caller, existing.ProjectID) {
			return
		}
		var in store.CreativePipelineInput
		if !creativeBody(w, r, &in) {
			return
		}
		out, err := d.Store.UpdateCreativePipeline(id, in)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /v1/creative/runs", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		out, err := d.Store.ListCreativeRuns(r.URL.Query().Get("project_id"))
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("POST /v1/creative/runs", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		var in store.CreativeRunInput
		if !creativeBody(w, r, &in) || !creativeProjectWrite(w, caller, in.ProjectID) {
			return
		}
		out, err := d.Store.CreateCreativeRun(in)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 201, out)
	}))
	mux.HandleFunc("GET /v1/creative/runs/{run}", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		id, ok := creativeID(w, r.PathValue("run"), false)
		if !ok {
			return
		}
		out, err := d.Store.GetCreativeRun(id)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("GET /v1/creative/runs/{run}/context", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		run, ok := creativeID(w, r.PathValue("run"), false)
		if !ok {
			return
		}
		stage, ok := creativeID(w, r.URL.Query().Get("stage_id"), true)
		if !ok {
			return
		}
		item, ok := creativeID(w, r.URL.Query().Get("item_id"), true)
		if !ok {
			return
		}
		out, err := d.Store.ResolveCreativeContext(run, stage, item)
		if err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, out)
	}))
	mux.HandleFunc("PUT /v1/creative/runs/{run}/orchestrator", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		ctx, ok := creativeWriteContext(w, r, d, caller)
		if !ok {
			return
		}
		var in struct {
			SessionID string `json:"session_id"`
		}
		if !creativeBody(w, r, &in) {
			return
		}
		if err := d.Store.SetCreativeRunOrchestrator(ctx.RunID, in.SessionID); err != nil {
			creativeError(w, err)
			return
		}
		writeCreativeRun(w, d, ctx.RunID, 200)
	}))
	for _, path := range []string{"/v1/creative/runs/{run}/stages/{stage}/actions", "/v1/creative/runs/{run}/stages/{stage}/items/{item}/actions"} {
		mux.HandleFunc("POST "+path, creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
			ctx, ok := creativeWriteContext(w, r, d, caller)
			if !ok {
				return
			}
			var in store.CreativeAction
			if !creativeBody(w, r, &in) {
				return
			}
			out, err := d.Store.ApplyCreativeAction(ctx.RunID, ctx.StageID, ctx.ItemID, in, caller == nil)
			if err != nil {
				creativeError(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
	}
	for _, path := range []string{"/v1/creative/runs/{run}/stages/{stage}", "/v1/creative/runs/{run}/stages/{stage}/items/{item}/progress"} {
		mux.HandleFunc("PATCH "+path, creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
			ctx, ok := creativeWriteContext(w, r, d, caller)
			if !ok {
				return
			}
			var in store.CreativeProgress
			if !creativeBody(w, r, &in) {
				return
			}
			out, err := d.Store.UpdateCreativeProgress(ctx.RunID, ctx.StageID, ctx.ItemID, in)
			if err != nil {
				creativeError(w, err)
				return
			}
			writeJSON(w, 200, out)
		}))
	}
	mux.HandleFunc("POST /v1/creative/runs/{run}/stages/{stage}/items", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		ctx, ok := creativeWriteContext(w, r, d, caller)
		if !ok {
			return
		}
		var in store.CreativeItemInput
		if !creativeBody(w, r, &in) {
			return
		}
		if _, err := d.Store.CreateCreativeItem(ctx.RunID, ctx.StageID, in); err != nil {
			creativeError(w, err)
			return
		}
		writeCreativeRun(w, d, ctx.RunID, 201)
	}))
	mux.HandleFunc("PATCH /v1/creative/runs/{run}/stages/{stage}/items/{item}", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		ctx, ok := creativeWriteContext(w, r, d, caller)
		if !ok {
			return
		}
		var in store.CreativeItemInput
		if !creativeBody(w, r, &in) {
			return
		}
		if _, err := d.Store.UpdateCreativeItem(ctx.RunID, ctx.StageID, ctx.ItemID, in); err != nil {
			creativeError(w, err)
			return
		}
		writeCreativeRun(w, d, ctx.RunID, 200)
	}))
	mux.HandleFunc("DELETE /v1/creative/runs/{run}/stages/{stage}/items/{item}", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
		ctx, ok := creativeWriteContext(w, r, d, caller)
		if !ok {
			return
		}
		if err := d.Store.DeleteCreativeItem(ctx.RunID, ctx.StageID, ctx.ItemID); err != nil {
			creativeError(w, err)
			return
		}
		writeCreativeRun(w, d, ctx.RunID, 200)
	}))
}
func creativeWriteContext(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session) (store.CreativeContext, bool) {
	var zero store.CreativeContext
	run, ok := creativeID(w, r.PathValue("run"), false)
	if !ok {
		return zero, false
	}
	stage, ok := creativeID(w, r.PathValue("stage"), true)
	if !ok {
		return zero, false
	}
	item, ok := creativeID(w, r.PathValue("item"), true)
	if !ok {
		return zero, false
	}
	ctx, err := d.Store.ResolveCreativeContext(run, stage, item)
	if err != nil {
		creativeError(w, err)
		return zero, false
	}
	if !creativeProjectWrite(w, caller, ctx.ProjectID) {
		return zero, false
	}
	return ctx, true
}
func writeCreativeRun(w http.ResponseWriter, d Deps, id int64, status int) {
	out, err := d.Store.GetCreativeRun(id)
	if err != nil {
		creativeError(w, err)
		return
	}
	writeJSON(w, status, out)
}
