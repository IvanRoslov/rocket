package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func putJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytesReader(b))
	if err != nil {
		t.Fatalf("build PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	return resp
}

func TestGetSettingsMaskingTable(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"absent", "", ""},
		{"short", "abc", "set"},
		{"exactly eight", "12345678", "set"},
		{"long", "ghp_1234567890abcdef", "ghp_…cdef"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.Open(filepath.Join(dir, "rocket.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()

			d := testDeps(t, nil)
			d.Store = st
			if tc.token != "" {
				if err := st.SetSetting("github_token", tc.token); err != nil {
					t.Fatalf("SetSetting: %v", err)
				}
			}

			srv := httptest.NewServer(NewHandler(d))
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/v1/settings")
			if err != nil {
				t.Fatalf("GET /v1/settings: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["github_token"] != tc.want {
				t.Errorf("github_token = %q, want %q", body["github_token"], tc.want)
			}
		})
	}
}

func TestPutSettingsValidTokenStoresAndReturnsLogin(t *testing.T) {
	ghStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ghp_valid" {
			t.Fatalf("Authorization header = %q, want Bearer ghp_valid", got)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "ivanroslov"})
	}))
	defer ghStub.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	d := testDeps(t, nil)
	d.Store = st
	d.Cfg.GithubAPIBase = ghStub.URL

	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]string{"github_token": "ghp_valid"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["login"] != "ivanroslov" {
		t.Errorf("login = %q, want ivanroslov", body["login"])
	}
	if body["github_token"] == "" || body["github_token"] == "ghp_valid" {
		t.Errorf("github_token = %q, want masked, non-empty, non-raw", body["github_token"])
	}

	stored, err := st.GetSetting("github_token")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if stored != "ghp_valid" {
		t.Errorf("stored token = %q, want ghp_valid", stored)
	}
}

func TestPutSettingsInvalidTokenNotStored(t *testing.T) {
	ghStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ghStub.Close()

	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	d := testDeps(t, nil)
	d.Store = st
	d.Cfg.GithubAPIBase = ghStub.URL

	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]string{"github_token": "ghp_bad"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "invalid_token" {
		t.Errorf("code = %q, want invalid_token", eb.Error.Code)
	}

	if _, err := st.GetSetting("github_token"); err != store.ErrNotFound {
		t.Errorf("GetSetting after invalid PUT: err = %v, want ErrNotFound", err)
	}
}

func TestPutSettingsGithubUnreachable(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	d := testDeps(t, nil)
	d.Store = st
	// Point at a stub that is immediately closed, so the request fails at
	// the transport level.
	deadStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	d.Cfg.GithubAPIBase = deadStub.URL
	deadStub.Close()

	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]string{"github_token": "ghp_whatever"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "github_unreachable" {
		t.Errorf("code = %q, want github_unreachable", eb.Error.Code)
	}

	if _, err := st.GetSetting("github_token"); err != store.ErrNotFound {
		t.Errorf("GetSetting after unreachable PUT: err = %v, want ErrNotFound", err)
	}
}

func TestPutSettingsEmptyTokenDeletes(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.SetSetting("github_token", "ghp_existing"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	d := testDeps(t, nil)
	d.Store = st

	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]string{"github_token": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["github_token"] != "" {
		t.Errorf("github_token = %q, want empty", body["github_token"])
	}

	if _, err := st.GetSetting("github_token"); err != store.ErrNotFound {
		t.Errorf("GetSetting after delete-via-PUT: err = %v, want ErrNotFound", err)
	}
}
