package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanRoslov/rocket/internal/preview"
	"github.com/IvanRoslov/rocket/internal/store"
)

func previewAPIFixture(t *testing.T) (Deps, http.Handler, store.CreativeRun) {
	t.Helper()
	d := authTestDeps(t)
	if err := d.Store.AddRepo(store.Repo{ID: "previewrepo", Path: t.TempDir(), DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"previewproject", "otherproject"} {
		if err := d.Store.AddProject(store.Project{ID: id, Name: id, MainRepo: "previewrepo"}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{ProjectID: "previewproject", Name: "landing", Stages: []store.CreativeStageDefinition{{Name: "draft", Approval: "auto", Repeated: true}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "previewproject", PipelineID: p.ID, Name: "landing", Brief: "Build a landing page"})
	if err != nil {
		t.Fatal(err)
	}
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	d.Cfg.PreviewPorts = []int{5100}
	initializePreviews(&d)
	return d, NewHandler(d), run
}
func previewCall(t *testing.T, h http.Handler, method, path, session string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	req := httptest.NewRequest(method, path, &b)
	if session != "" {
		req.Header.Set(sessionHeader, session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func TestCreativePreviewContextAndPrelaunchAccess(t *testing.T) {
	d, h, run := previewAPIFixture(t)
	in := map[string]any{"run_id": run.ID, "stage_id": run.Stages[0].ID, "port": 5100}
	rec := previewCall(t, h, "POST", "/v1/creative/previews", "", in)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var a preview.Access
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.BasePath == "" || a.Status != "unavailable" || a.URL != "https://preview.example.net"+a.BasePath {
		t.Fatalf("%+v", a)
	}
	query := fmt.Sprintf("/v1/creative/previews?run_id=%d&stage_id=%d", run.ID, run.Stages[0].ID)
	rec = previewCall(t, h, "GET", query, "", nil)
	var list []preview.Access
	json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = previewCall(t, h, "GET", fmt.Sprintf("/v1/creative/previews?run_id=%d", run.ID), "", nil)
	if rec.Code != 200 || rec.Body.String() != "[]\n" {
		t.Fatalf("run-only should not mix stage previews: %s", rec.Body)
	}
	rec = previewCall(t, h, "DELETE", "/v1/creative/previews/"+a.ID, "", nil)
	if rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err := d.Previews.Lookup(a.ID); err == nil {
		t.Fatal("not revoked")
	}
}
func TestCreativePreviewInvalidContextsAndPorts(t *testing.T) {
	_, h, run := previewAPIFixture(t)
	for _, in := range []map[string]any{
		{"run_id": run.ID, "stage_id": 99999, "port": 5100},
		{"run_id": run.ID, "item_id": 99999, "port": 5100},
		{"run_id": 99999, "port": 5100},
		{"run_id": run.ID, "port": 22},
		{"run_id": run.ID, "port": 5100, "url": "http://other:80"},
		{"run_id": -1, "port": 5100},
	} {
		rec := previewCall(t, h, "POST", "/v1/creative/previews", "", in)
		if rec.Code < 400 {
			t.Fatalf("accepted %+v: %s", in, rec.Body)
		}
	}
	for _, q := range []string{"", "?run_id=oops", "?run_id=1&stage_id=-1"} {
		rec := previewCall(t, h, "GET", "/v1/creative/previews"+q, "", nil)
		if rec.Code < 400 {
			t.Fatalf("accepted %s", q)
		}
	}
}
func TestCreativePreviewCallerAuthorization(t *testing.T) {
	d, h, run := previewAPIFixture(t)
	for _, s := range []store.Session{{ID: "same", ProjectID: "previewproject", Kind: "agent"}, {ID: "other", ProjectID: "otherproject", Kind: "agent"}} {
		if err := d.Store.AddSession(s); err != nil {
			t.Fatal(err)
		}
	}
	in := map[string]any{"run_id": run.ID, "port": 5100}
	for _, who := range []string{"other", "unknown"} {
		rec := previewCall(t, h, "POST", "/v1/creative/previews", who, in)
		if rec.Code != 403 && rec.Code != 401 {
			t.Fatalf("%s: %d %s", who, rec.Code, rec.Body)
		}
	}
	rec := previewCall(t, h, "POST", "/v1/creative/previews", "same", in)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var a preview.Access
	json.Unmarshal(rec.Body.Bytes(), &a)
	rec = previewCall(t, h, "DELETE", "/v1/creative/previews/"+a.ID, "other", nil)
	if rec.Code != 403 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = previewCall(t, h, "GET", fmt.Sprintf("/v1/creative/previews?run_id=%d", run.ID), "other", nil)
	if rec.Code != 403 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	tcp := requireAuth(d, h)
	rec = previewCall(t, tcp, "GET", "http://127.0.0.1/v1/creative/previews/setup", "", nil)
	if rec.Code != 401 {
		t.Fatalf("unauthenticated setup %d", rec.Code)
	}
	token, _ := pairedToken(t, h)
	req := httptest.NewRequest("GET", "http://127.0.0.1/v1/creative/previews/setup", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = serve(tcp, req)
	if rec.Code != 200 {
		t.Fatalf("authenticated setup %d %s", rec.Code, rec.Body)
	}
}

func TestCreativePreviewCannotClaimAnotherProjectsPort(t *testing.T) {
	d, h, run := previewAPIFixture(t)
	first := previewCall(t, h, "POST", "/v1/creative/previews", "", map[string]any{"run_id": run.ID, "port": 5100})
	if first.Code != 201 {
		t.Fatal(first.Body)
	}
	var original preview.Access
	json.Unmarshal(first.Body.Bytes(), &original)
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{ProjectID: "otherproject", Name: "other", Stages: []store.CreativeStageDefinition{{Name: "landing", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "otherproject", PipelineID: p.ID, Name: "other", Brief: "another project"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Store.AddSession(store.Session{ID: "other", Kind: "agent", ProjectID: "otherproject"}); err != nil {
		t.Fatal(err)
	}
	rec := previewCall(t, h, "POST", "/v1/creative/previews", "other", map[string]any{"run_id": other.ID, "port": 5100})
	if rec.Code != 400 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if _, err = d.Previews.Lookup(original.ID); err != nil {
		t.Fatal("original preview was revoked")
	}
}
