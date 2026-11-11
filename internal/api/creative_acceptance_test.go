package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/preview"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

type acceptanceRuntime struct {
	sessFakeRuntime
	live               map[string]bool
	failStop, failList bool
}

func (r *acceptanceRuntime) Destroy(_ context.Context, h runtime.Handle) error {
	if r.failStop {
		return errors.New("injected stop error")
	}
	delete(r.live, h.Name)
	return nil
}
func (r *acceptanceRuntime) List(context.Context) ([]string, error) {
	if r.failList {
		return nil, errors.New("injected inventory error")
	}
	out := []string{}
	for n := range r.live {
		out = append(out, n)
	}
	return out, nil
}
func (r *acceptanceRuntime) Alive(_ context.Context, h runtime.Handle) bool { return r.live[h.Name] }
func acceptanceFixture(t *testing.T) (Deps, store.CreativeRun, *acceptanceRuntime, *creativeCloseWorkspace) {
	t.Helper()
	d, r, _, ws := creativeCloseFixture(t)
	rt := &acceptanceRuntime{live: map[string]bool{}}
	for _, id := range []string{"orch", "worker", "sibling"} {
		s, _ := d.Store.GetSession(id)
		rt.live[s.TmuxName] = true
	}
	d.Manager = session.NewManager(d.Store, d.Bus, rt, ws, d.Cfg)
	return d, r, rt, ws
}
func finishStages(t *testing.T, d Deps, r store.CreativeRun) {
	t.Helper()
	for _, a := range []string{"start", "submit"} {
		if _, e := d.Store.ApplyCreativeAction(r.ID, r.Stages[0].ID, 0, store.CreativeAction{Action: a}, false); e != nil {
			t.Fatal(e)
		}
	}
}
func TestCreativeAcceptanceHumanEligibilityAndScopedArchive(t *testing.T) {
	d, r, rt, ws := acceptanceFixture(t)
	h := NewHandler(d)
	path := fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID)
	if rec := creativeCall(h, "POST", path, "", "{}"); rec.Code != 409 {
		t.Fatalf("unfinished: %d %s", rec.Code, rec.Body.String())
	}
	finishStages(t, d, r)
	if rec := creativeCall(h, "POST", path, "orch", "{}"); rec.Code != 403 {
		t.Fatalf("agent: %d", rec.Code)
	}
	other := addTestSession(t, d, "other", "worker", "film")
	rt.live[other.TmuxName] = true
	for i := 0; i < 2; i++ {
		rec := creativeCall(h, "POST", path, "", "{}")
		if rec.Code != 200 {
			t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
		}
	}
	got, _ := d.Store.GetCreativeRun(r.ID)
	if got.ClosureState != "accepted" || got.AcceptedAt == 0 {
		t.Fatalf("archive: %+v", got)
	}
	for _, id := range []string{"orch", "worker", "sibling"} {
		s, _ := d.Store.GetSession(id)
		if s.State != "killed" || rt.live[s.TmuxName] {
			t.Fatalf("not stopped: %+v", s)
		}
	}
	if !reflect.DeepEqual(rt.live, map[string]bool{other.TmuxName: true}) || len(ws.deleted) != 0 {
		t.Fatalf("isolation/files: %v %v", rt.live, ws.deleted)
	}
	for _, p := range []string{fmt.Sprintf("/v1/creative/runs/%d", r.ID), fmt.Sprintf("/v1/creative/runs/%d/feed", r.ID), fmt.Sprintf("/v1/creative/runs/%d/sessions", r.ID)} {
		rec := creativeCall(h, "GET", p, "", "")
		if rec.Code != 200 {
			t.Fatalf("read %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	mutations := []struct{ method, path, body string }{
		{"POST", fmt.Sprintf("/v1/creative/runs/%d/stages/%d/actions", r.ID, r.Stages[0].ID), `{"action":"reopen"}`},
		{"POST", fmt.Sprintf("/v1/creative/runs/%d/feed", r.ID), `{"body":"late"}`},
		{"POST", fmt.Sprintf("/v1/creative/runs/%d/sessions", r.ID), `{}`},
		{"POST", "/v1/sessions/orch/restore", `{}`},
		{"POST", "/v1/sessions/orch/kill", `{"cleanup":true}`},
		{"POST", "/v1/messages", `{"to":"orch","body":"late"}`},
	}
	for _, m := range mutations {
		rec := creativeCall(h, m.method, m.path, "", m.body)
		if rec.Code != 409 {
			t.Errorf("mutation %s: %d %s", m.path, rec.Code, rec.Body.String())
		}
	}
	if err := d.Manager.Restore(context.Background(), "orch", session.RestoreOpts{}); err == nil {
		t.Fatal("direct manager restore allowed")
	}
}
func TestCreativeAcceptanceIncompleteClosureRetry(t *testing.T) {
	for _, which := range []string{"stop", "inventory", "save"} {
		t.Run(which, func(t *testing.T) {
			d, r, rt, _ := acceptanceFixture(t)
			finishStages(t, d, r)
			h := NewHandler(d)
			path := fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID)
			if which == "stop" {
				rt.failStop = true
			}
			if which == "inventory" {
				rt.failList = true
			}
			db, err := sql.Open("sqlite", filepath.Join(d.Cfg.Home, "rocket.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if which == "save" {
				_, err := db.Exec(`CREATE TRIGGER fail_accept BEFORE UPDATE OF closure_state ON creative_runs WHEN NEW.closure_state='accepted' BEGIN SELECT RAISE(ABORT,'injected save failure'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			rec := creativeCall(h, "POST", path, "", "{}")
			if rec.Code < 400 {
				t.Fatalf("false success: %s", rec.Body.String())
			}
			got, _ := d.Store.GetCreativeRun(r.ID)
			if got.ClosureState != "closing" || got.AcceptedAt != 0 || got.ClosureError == "" {
				t.Fatalf("missing recovery: %+v", got)
			}
			rec = creativeCall(h, "GET", "/v1/creative/runs?project_id=film", "", "")
			var list []store.CreativeRun
			json.Unmarshal(rec.Body.Bytes(), &list)
			if len(list) != 1 {
				t.Fatal("incomplete closure vanished")
			}
			rt.failStop = false
			rt.failList = false
			if which == "save" {
				if _, e := db.Exec(`DROP TRIGGER fail_accept`); e != nil {
					t.Fatal(e)
				}
			}
			rec = creativeCall(h, "POST", path, "", "{}")
			if rec.Code != http.StatusOK {
				t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
			}
			got, _ = d.Store.GetCreativeRun(r.ID)
			if got.ClosureState != "accepted" || got.ClosureError != "" {
				t.Fatalf("retry state: %+v", got)
			}
		})
	}
}

func creativeCall(h http.Handler, method, path, caller, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if caller != "" {
		r.Header.Set(sessionHeader, caller)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A runtime returning nil without actually stopping must never produce an archive.
type lingeringAcceptanceRuntime struct{ *acceptanceRuntime }

func (r lingeringAcceptanceRuntime) Destroy(context.Context, runtime.Handle) error { return nil }
func TestCreativeAcceptanceRequiresRuntimeAbsence(t *testing.T) {
	d, r, rt, ws := acceptanceFixture(t)
	finishStages(t, d, r)
	d.Manager = session.NewManager(d.Store, d.Bus, lingeringAcceptanceRuntime{rt}, ws, d.Cfg)
	rec := creativeCall(NewHandler(d), "POST", fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID), "", "{}")
	if rec.Code < 400 {
		t.Fatal("destroy acknowledgment incorrectly proves stopped")
	}
	got, _ := d.Store.GetCreativeRun(r.ID)
	if got.ClosureState != "closing" {
		t.Fatalf("hidden incomplete closure: %+v", got)
	}
}
func TestCreativeAcceptanceRejectsPreviewMutation(t *testing.T) {
	d, r, _, _ := acceptanceFixture(t)
	finishStages(t, d, r)
	creativeRequest(t, d, "POST", fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID), "{}", "", 200)
	creativeRequest(t, d, "POST", "/v1/creative/previews", fmt.Sprintf(`{"run_id":%d,"port":3000}`, r.ID), "", 409)
}

func TestCreativeAcceptanceDisablesExistingInteractivePreview(t *testing.T) {
	d, r, _, _ := acceptanceFixture(t)
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	d.Cfg.PreviewPorts = []int{5100}
	initializePreviews(&d)
	a, err := d.Previews.Register(preview.Context{RunID: r.ID}, 5100)
	if err != nil {
		t.Fatal(err)
	}
	finishStages(t, d, r)
	creativeRequest(t, d, "POST", fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID), "{}", "", 200)
	rec := creativeCall(creativePreviewHandler(d), "POST", a.BasePath, "", "change")
	if rec.Code != 410 {
		t.Fatalf("interactive preview still routed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreativeAcceptanceRetainsDownloadsAndRejectsArtifactChanges(t *testing.T) {
	d, r, _, _ := acceptanceFixture(t)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(r.ID, r.Stages[0].ID, "Final film", "result", "film.mp4"), "video/mp4", "", strings.NewReader("retained-film")))
	finishStages(t, d, r)
	creativeRequest(t, d, "POST", fmt.Sprintf("/v1/creative/runs/%d/accept", r.ID), "{}", "", 200)
	for _, path := range []string{p.Version.ContentURL, p.Version.DownloadURL} {
		rec := creativeCall(h, "GET", path, "", "")
		if rec.Code != 200 || rec.Body.String() != "retained-film" {
			t.Fatalf("retained file %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	creativeRequest(t, d, "PUT", fmt.Sprintf("/v1/creative/artifacts/%d/current", p.Artifact.ID), fmt.Sprintf(`{"version_id":%d}`, p.Version.ID), "", 409)
	rec := artifactRequest(h, "POST", fmt.Sprintf("/v1/creative/artifacts/%d/versions?filename=late.mp4", p.Artifact.ID), "video/mp4", "", strings.NewReader("late"))
	if rec.Code != 409 {
		t.Fatalf("late artifact: %d %s", rec.Code, rec.Body.String())
	}
}
