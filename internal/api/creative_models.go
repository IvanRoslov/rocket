package api

import (
	"net/http"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

func validateCreativeAllowedProfiles(w http.ResponseWriter, d Deps, names []string) bool {
	missing, err := unknownProfiles(d.Store, names)
	if err != nil {
		creativeError(w, err)
		return false
	}
	if len(missing) > 0 {
		writeErr(w, http.StatusBadRequest, "profile_not_found", "unknown profiles: "+strings.Join(missing, ", "))
		return false
	}
	return true
}

func registerCreativeModelRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("PUT /v1/creative/runs/{run}/models", creativeRoute(d, func(w http.ResponseWriter, r *http.Request, _ *store.Session) {
		if requireHuman(w, r, d) {
			return
		}
		runID, ok := creativeID(w, r.PathValue("run"), false)
		if !ok {
			return
		}
		var in struct {
			AllowedProfiles *[]string `json:"allowed_profiles"`
		}
		if !creativeBody(w, r, &in) {
			return
		}
		if in.AllowedProfiles == nil {
			writeErr(w, 400, "bad_request", "allowed_profiles must be an array (use [] to clear)")
			return
		}
		if !validateCreativeAllowedProfiles(w, d, *in.AllowedProfiles) {
			return
		}
		if err := d.Store.SetCreativeRunAllowedProfiles(runID, *in.AllowedProfiles); err != nil {
			creativeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"allowed_profiles": *in.AllowedProfiles})
	}))
}
