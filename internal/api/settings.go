package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// settingsValidateTimeout is the timeout for validating GitHub tokens.
// It can be overridden in tests.
var settingsValidateTimeout = 10 * time.Second

// settingsHTTPClient is the HTTP client used for validating GitHub tokens,
// configured with a timeout.
var settingsHTTPClient = &http.Client{Timeout: 10 * time.Second}

// registerSettingsRoutes wires the /v1/settings routes onto mux.
func registerSettingsRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		handleGetSettings(w, r, d)
	})
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		handlePutSettings(w, r, d)
	})
}

// maskToken masks a GitHub token for display: tokens longer than 8
// characters show their first 4 and last 4 characters separated by an
// ellipsis; shorter (but non-empty) tokens are masked to the fixed string
// "set" so no part of a short secret leaks; an empty token masks to "".
func maskToken(token string) string {
	switch {
	case token == "":
		return ""
	case len(token) > 8:
		return token[:4] + "…" + token[len(token)-4:]
	default:
		return "set"
	}
}

// handleGetSettings serves GET /v1/settings: the masked GitHub token (or ""
// if unset) and the orchestrator_brainstorm_custom toggle as a bool.
func handleGetSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	token, err := d.Store.GetSetting("github_token")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	custom, err := d.Store.OrchestratorBrainstormCustom()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"github_token":                   maskToken(token),
		"orchestrator_brainstorm_custom": custom,
	})
}

// putSettingsRequest is the PUT /v1/settings body. Every field is optional
// and only the ones present are applied, so flipping the brainstorm toggle
// never touches the token (and vice versa).
type putSettingsRequest struct {
	GithubToken                  *string `json:"github_token"`
	OrchestratorBrainstormCustom *bool   `json:"orchestrator_brainstorm_custom"`
}

type githubUserResponse struct {
	Login string `json:"login"`
}

// validateGithubToken checks token against GitHub's /user endpoint using
// apiBase, returning the authenticated login on success. The request is bounded
// by settingsValidateTimeout to prevent hanging on unresponsive GitHub endpoints.
//
// Returns a *store's ErrNotFound-free* error classification via the two
// bool results: invalid indicates the token was rejected by GitHub (401/403,
// caller should respond 400 invalid_token); unreachable indicates a
// network/transport error, context timeout, or any non-2xx status from GitHub
// (caller should respond 502 github_unreachable). Any non-200/401/403 status
// is treated as unreachable, since it's not something the caller's local
// retry logic should treat as a bad token.
func validateGithubToken(apiBase, token string) (login string, invalid bool, unreachable bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), settingsValidateTimeout)
	defer cancel()

	req, buildErr := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/user", nil)
	if buildErr != nil {
		return "", false, true, buildErr
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, doErr := settingsHTTPClient.Do(req)
	if doErr != nil {
		return "", false, true, doErr
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", true, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, true, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", false, true, readErr
	}
	var u githubUserResponse
	if jsonErr := json.Unmarshal(body, &u); jsonErr != nil {
		return "", false, true, jsonErr
	}
	return u.Login, false, false, nil
}

// handlePutSettings serves PUT /v1/settings {github_token?,
// orchestrator_brainstorm_custom?}. An empty token deletes the stored
// setting; a non-empty token is validated against GitHub's /user endpoint
// before being stored. The token is checked first so a rejected token leaves
// the toggle unchanged too. The response carries both settings (masked
// token) plus "login" when a token was validated.
func handlePutSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	var req putSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.GithubToken == nil && req.OrchestratorBrainstormCustom == nil {
		writeErr(w, http.StatusBadRequest, "bad_request",
			"nothing to update: send github_token and/or orchestrator_brainstorm_custom")
		return
	}

	resp := map[string]any{}
	if req.GithubToken != nil {
		token := *req.GithubToken
		if token == "" {
			if err := d.Store.DeleteSetting("github_token"); err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		} else {
			login, invalid, unreachable, err := validateGithubToken(d.Cfg.GithubAPIBase, token)
			if invalid {
				writeErr(w, http.StatusBadRequest, "invalid_token", "GitHub rejected the token")
				return
			}
			if unreachable || err != nil {
				writeErr(w, http.StatusBadGateway, "github_unreachable", "could not reach GitHub to validate token")
				return
			}
			if err := d.Store.SetSetting("github_token", token); err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			resp["login"] = login
		}
	}

	if req.OrchestratorBrainstormCustom != nil {
		if err := d.Store.SetSetting(store.SettingOrchestratorBrainstormCustom,
			strconv.FormatBool(*req.OrchestratorBrainstormCustom)); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	token, err := d.Store.GetSetting("github_token")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	custom, err := d.Store.OrchestratorBrainstormCustom()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp["github_token"] = maskToken(token)
	resp["orchestrator_brainstorm_custom"] = custom
	writeJSON(w, http.StatusOK, resp)
}
