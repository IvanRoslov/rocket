package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

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

// handleGetSettings serves GET /v1/settings, returning the masked GitHub
// token (or "" if unset).
func handleGetSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	token, err := d.Store.GetSetting("github_token")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"github_token": maskToken(token)})
}

type putSettingsRequest struct {
	GithubToken string `json:"github_token"`
}

type githubUserResponse struct {
	Login string `json:"login"`
}

// validateGithubToken checks token against GitHub's /user endpoint using
// apiBase, returning the authenticated login on success.
//
// Returns a *store's ErrNotFound-free* error classification via the two
// bool results: invalid indicates the token was rejected by GitHub (401/403,
// caller should respond 400 invalid_token); unreachable indicates a
// network/transport error or a 5xx from GitHub (caller should respond 502
// github_unreachable). Any other non-2xx status is also treated as
// unreachable, since it's not something the caller's local retry logic
// should treat as a bad token.
func validateGithubToken(apiBase, token string) (login string, invalid bool, unreachable bool, err error) {
	req, buildErr := http.NewRequest(http.MethodGet, apiBase+"/user", nil)
	if buildErr != nil {
		return "", false, true, buildErr
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, doErr := http.DefaultClient.Do(req)
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

// handlePutSettings serves PUT /v1/settings {github_token}. An empty token
// deletes the stored setting. A non-empty token is validated against
// GitHub's /user endpoint before being stored.
func handlePutSettings(w http.ResponseWriter, r *http.Request, d Deps) {
	var req putSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.GithubToken == "" {
		if err := d.Store.DeleteSetting("github_token"); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"github_token": ""})
		return
	}

	login, invalid, unreachable, err := validateGithubToken(d.Cfg.GithubAPIBase, req.GithubToken)
	if invalid {
		writeErr(w, http.StatusBadRequest, "invalid_token", "GitHub rejected the token")
		return
	}
	if unreachable || err != nil {
		writeErr(w, http.StatusBadGateway, "github_unreachable", "could not reach GitHub to validate token")
		return
	}

	if err := d.Store.SetSetting("github_token", req.GithubToken); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"github_token": maskToken(req.GithubToken),
		"login":        login,
	})
}
