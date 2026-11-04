package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/preview"
	"github.com/IvanRoslov/rocket/internal/store"
)

func registerCreativePreviewRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/creative/previews/setup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, d.Previews.Setup())
	})
	mux.HandleFunc("POST /v1/creative/previews", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			preview.Context
			Port int `json:"port"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeErr(w, 400, "invalid_request", "valid preview context and port required")
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeErr(w, 400, "invalid_request", "one JSON object required")
			return
		}
		c, ok := resolvePreviewContext(w, r, d, in.Context)
		if !ok {
			return
		}
		out, err := d.Previews.Register(c, in.Port)
		if err != nil {
			writePreviewErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})
	mux.HandleFunc("GET /v1/creative/previews", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		q := r.URL.Query()
		var c preview.Context
		for name, p := range map[string]*int64{"run_id": &c.RunID, "stage_id": &c.StageID, "item_id": &c.ItemID} {
			value := q.Get(name)
			if value == "" && name != "run_id" {
				continue
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n <= 0 {
				writeErr(w, 400, "invalid_context", name+" must be a positive integer")
				return
			}
			*p = n
		}
		c, ok := resolvePreviewContext(w, r, d, c)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, d.Previews.List(r.Context(), c))
	})
	mux.HandleFunc("DELETE /v1/creative/previews/{id}", func(w http.ResponseWriter, r *http.Request) {
		a, err := d.Previews.Lookup(r.PathValue("id"))
		if err != nil {
			writePreviewErr(w, err)
			return
		}
		if _, ok := resolvePreviewContext(w, r, d, a.Context); !ok {
			return
		}
		if err := d.Previews.Revoke(a.ID); err != nil {
			writePreviewErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
func resolvePreviewContext(w http.ResponseWriter, r *http.Request, d Deps, in preview.Context) (preview.Context, bool) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return preview.Context{}, false
	}
	c, err := d.Store.ResolveCreativeContext(in.RunID, in.StageID, in.ItemID)
	if err != nil {
		writePreviewErr(w, err)
		return preview.Context{}, false
	}
	// Persistent agents do not get implicit cross-project preview access.
	if caller != nil && caller.ProjectID != c.ProjectID {
		writeErr(w, 403, "forbidden", "preview context belongs to another project")
		return preview.Context{}, false
	}
	return preview.Context{RunID: c.RunID, StageID: c.StageID, ItemID: c.ItemID}, true
}
func writePreviewErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, preview.ErrNotFound):
		writeErr(w, 404, "not_found", "preview or creative context not found")
	case errors.Is(err, store.ErrCreativeInvalid), errors.Is(err, preview.ErrInvalid):
		writeErr(w, 400, "invalid_preview", err.Error())
	default:
		writeErr(w, 500, "internal_error", "preview request failed")
	}
}
