package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func settingsDeps(t *testing.T) Deps {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	d := testDeps(t, nil)
	d.Store = st
	return d
}

func getSettingsBody(t *testing.T, base string) map[string]any {
	t.Helper()
	resp, err := http.Get(base + "/v1/settings")
	if err != nil {
		t.Fatalf("GET /v1/settings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestGetSettingsBrainstormToggleDefaultsFalse(t *testing.T) {
	d := settingsDeps(t)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	body := getSettingsBody(t, srv.URL)
	v, ok := body["orchestrator_brainstorm_custom"].(bool)
	if !ok || v {
		t.Errorf("orchestrator_brainstorm_custom = %#v, want false (bool)", body["orchestrator_brainstorm_custom"])
	}
}

// Flipping the toggle alone must leave the stored GitHub token untouched —
// the old handler treated a missing github_token as "" and deleted it.
func TestPutSettingsBrainstormToggleKeepsToken(t *testing.T) {
	d := settingsDeps(t)
	if err := d.Store.SetSetting("github_token", "ghp_1234567890abcdef"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]any{"orchestrator_brainstorm_custom": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}
	var put map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&put); err != nil {
		t.Fatal(err)
	}
	if put["orchestrator_brainstorm_custom"] != true {
		t.Errorf("PUT response toggle = %#v", put["orchestrator_brainstorm_custom"])
	}
	if put["github_token"] != "ghp_…cdef" {
		t.Errorf("PUT response github_token = %#v", put["github_token"])
	}

	tok, err := d.Store.GetSetting("github_token")
	if err != nil || tok != "ghp_1234567890abcdef" {
		t.Errorf("token after toggle PUT = %q, %v", tok, err)
	}
	if v, _ := d.Store.GetSetting(store.SettingOrchestratorBrainstormCustom); v != "true" {
		t.Errorf("stored toggle = %q, want \"true\"", v)
	}

	body := getSettingsBody(t, srv.URL)
	if body["orchestrator_brainstorm_custom"] != true || body["github_token"] != "ghp_…cdef" {
		t.Errorf("GET after PUT = %#v", body)
	}

	resp2 := putJSON(t, srv.URL+"/v1/settings", map[string]any{"orchestrator_brainstorm_custom": false})
	resp2.Body.Close()
	if v, _ := d.Store.GetSetting(store.SettingOrchestratorBrainstormCustom); v != "false" {
		t.Errorf("stored toggle after off = %q, want \"false\"", v)
	}
}

func TestPutSettingsEmptyBodyRejected(t *testing.T) {
	d := settingsDeps(t)
	if err := d.Store.SetSetting("github_token", "ghp_1234567890abcdef"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp := putJSON(t, srv.URL+"/v1/settings", map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT {} status = %d, want 400", resp.StatusCode)
	}
	if tok, _ := d.Store.GetSetting("github_token"); tok == "" {
		t.Error("PUT {} deleted the token")
	}
}
