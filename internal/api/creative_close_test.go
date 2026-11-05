package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

type creativeCloseRuntime struct {
	sessFakeRuntime
	stopped []string
}

func (r *creativeCloseRuntime) Destroy(_ context.Context, h runtime.Handle) error {
	r.stopped = append(r.stopped, h.Name)
	return nil
}

type creativeCloseWorkspace struct {
	sessFakeWorkspace
	deleted []string
}

func (w *creativeCloseWorkspace) Destroy(_ context.Context, _ store.Repo, id string) error {
	w.deleted = append(w.deleted, id)
	return nil
}

func creativeCloseFixture(t *testing.T) (Deps, store.CreativeRun, *creativeCloseRuntime, *creativeCloseWorkspace) {
	t.Helper()
	d := sessionsTestDeps(t)
	addTestProject(t, d, "film")
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{Name: "Film", Stages: []store.CreativeStageDefinition{{Name: "Scene", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "film", PipelineID: p.ID, Name: "Film", Brief: "Make film"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"orch", "worker", "sibling"} {
		kind := "worker"
		b := store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID}
		if id == "orch" {
			kind = "orchestrator"
			b.StageID = 0
		}
		s := addTestSession(t, d, id, kind, "film")
		if kind == "worker" {
			s.ParentID = "orch"
			if err := d.Store.UpdateSession(s); err != nil {
				t.Fatal(err)
			}
		}
		if err := d.Store.BindCreativeSession(id, b); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Store.SetCreativeRunOrchestrator(run.ID, "orch"); err != nil {
		t.Fatal(err)
	}
	rt, ws := &creativeCloseRuntime{}, &creativeCloseWorkspace{}
	d.Manager = session.NewManager(d.Store, d.Bus, rt, ws, d.Cfg)
	return d, run, rt, ws
}

// Missing ownership checks would allow the forbidden requests to stop real workers.
func TestCreativeCloseAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, caller, target string
		status               int
		change               func(*testing.T, Deps, store.CreativeRun)
	}{
		{name: "own worker", caller: "orch", target: "worker", status: 200},
		{name: "replacement owns predecessor worker", caller: "replacement", target: "worker", status: 200, change: func(t *testing.T, d Deps, r store.CreativeRun) {
			addTestSession(t, d, "replacement", "orchestrator", "film")
			if err := d.Store.BindCreativeSession("replacement", store.CreativeBinding{RunID: r.ID}); err != nil {
				t.Fatal(err)
			}
			if err := d.Store.SetCreativeRunOrchestrator(r.ID, "replacement"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "human", target: "worker", status: 403},
		{name: "worker caller", caller: "sibling", target: "worker", status: 403},
		{name: "self worker", caller: "worker", target: "worker", status: 403},
		{name: "orchestrator target", caller: "orch", target: "orch", status: 403},
		{name: "unknown caller", caller: "unknown", target: "worker", status: 401},
		{name: "missing target", caller: "orch", target: "missing", status: 404},
		{name: "unbound target", caller: "orch", target: "unbound", status: 403, change: func(t *testing.T, d Deps, _ store.CreativeRun) { addTestSession(t, d, "unbound", "worker", "film") }},
		{name: "unbound caller", caller: "unbound", target: "worker", status: 403, change: func(t *testing.T, d Deps, r store.CreativeRun) {
			addTestSession(t, d, "unbound", "orchestrator", "film")
			if err := d.Store.SetCreativeRunOrchestrator(r.ID, "unbound"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign run despite matching parent", caller: "orch", target: "foreign", status: 403, change: func(t *testing.T, d Deps, r store.CreativeRun) {
			other, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "film", PipelineID: r.PipelineID, Name: "Other", Brief: "Other"})
			if err != nil {
				t.Fatal(err)
			}
			s := addTestSession(t, d, "foreign", "worker", "film")
			s.ParentID = "orch"
			if err := d.Store.UpdateSession(s); err != nil {
				t.Fatal(err)
			}
			if err := d.Store.BindCreativeSession("foreign", store.CreativeBinding{RunID: other.ID, StageID: other.Stages[0].ID}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "stale caller", caller: "orch", target: "worker", status: 403, change: func(t *testing.T, d Deps, r store.CreativeRun) {
			addTestSession(t, d, "replacement", "orchestrator", "film")
			if err := d.Store.SetCreativeRunOrchestrator(r.ID, "replacement"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, run, rt, ws := creativeCloseFixture(t)
			if tc.change != nil {
				tc.change(t, d, run)
			}
			w := artifactRequest(NewHandler(d), "POST", fmt.Sprintf("/v1/creative/runs/%d/workers/%s/close", run.ID, tc.target), "", tc.caller, nil)
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == 200 {
				target, err := d.Store.GetSession(tc.target)
				if err != nil || target.State != "killed" {
					t.Fatalf("target %+v: %v", target, err)
				}
				if !reflect.DeepEqual(rt.stopped, []string{tc.target}) {
					t.Fatalf("runtime stops %v", rt.stopped)
				}
			} else if len(rt.stopped) != 0 {
				t.Fatalf("forbidden stop: %v", rt.stopped)
			}
			if len(ws.deleted) != 0 {
				t.Fatalf("workspace cleanup: %v", ws.deleted)
			}
			sibling, _ := d.Store.GetSession("sibling")
			if sibling.State != "running" {
				t.Fatalf("cascade: %+v", sibling)
			}
		})
	}
}

func TestCreativeCloseRejectsInvalidSessionMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		mutate   func(*store.Session)
	}{
		{"foreign target project", "worker", func(s *store.Session) { s.ProjectID = "foreign" }},
		{"foreign caller project", "orch", func(s *store.Session) { s.ProjectID = "foreign" }},
		{"development target", "worker", func(s *store.Session) { s.TaskID = 99 }},
		{"development caller", "orch", func(s *store.Session) { s.TaskID = 99 }},
		{"subtask target", "worker", func(s *store.Session) { s.SubtaskID = 99 }},
		{"stopped caller", "orch", func(s *store.Session) { s.State = "killed" }},
		{"caller worker kind", "orch", func(s *store.Session) { s.Kind = "worker" }},
		{"target orchestrator kind", "worker", func(s *store.Session) { s.Kind = "orchestrator" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, run, rt, _ := creativeCloseFixture(t)
			s, err := d.Store.GetSession(tc.id)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&s)
			if err := d.Store.UpdateSession(s); err != nil {
				t.Fatal(err)
			}
			w := artifactRequest(NewHandler(d), "POST", fmt.Sprintf("/v1/creative/runs/%d/workers/worker/close", run.ID), "", "orch", nil)
			if w.Code != 403 || len(rt.stopped) != 0 {
				t.Fatalf("status %d stops %v: %s", w.Code, rt.stopped, w.Body.String())
			}
		})
	}
}

// cleanup=true, cascade=true and repeated close must never remove retained work.
func TestCreativeClosePreservesWorkAndHistory(t *testing.T) {
	d, run, rt, ws := creativeCloseFixture(t)
	s, err := d.Store.GetSession("worker")
	if err != nil {
		t.Fatal(err)
	}
	s.WorktreePath = t.TempDir()
	if err := d.Store.UpdateSession(s); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(s.WorktreePath, "source.txt")
	if err := os.WriteFile(file, []byte("editable source"), 0600); err != nil {
		t.Fatal(err)
	}
	b := store.CreativeBinding{RunID: run.ID, StageID: run.Stages[0].ID}
	msg, err := d.Store.AddCreativeMessage(store.Message{FromSession: "worker", ToSession: "orch", Body: "result published"}, b)
	if err != nil {
		t.Fatal(err)
	}
	q, err := d.Store.AddCreativeQuestion(store.Question{AskedBy: "worker", Body: "Which result?"}, b)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(d)
	pub := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run.ID, b.StageID, "Result", "result", "result.txt"), "text/plain", "worker", strings.NewReader("published result")))
	before, err := d.Store.GetCreativeRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w := artifactRequest(h, "POST", fmt.Sprintf("/v1/creative/runs/%d/workers/worker/close?cleanup=true&cascade=true", run.ID), "", "orch", nil)
		if w.Code != 200 {
			t.Fatalf("close %d: %s", w.Code, w.Body.String())
		}
	}
	if !reflect.DeepEqual(rt.stopped, []string{"worker"}) || len(ws.deleted) != 0 {
		t.Fatalf("stops=%v cleanup=%v", rt.stopped, ws.deleted)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "editable source" {
		t.Fatalf("workspace file %q: %v", got, err)
	}
	if _, err := d.Store.GetMessage(msg); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Store.GetQuestion(q); err != nil {
		t.Fatal(err)
	}
	for kind, id := range map[string]int64{"message": msg, "question": q} {
		if got, err := d.Store.GetCreativeDiscussionContext(kind, id); err != nil || got.RunID != b.RunID || got.StageID != b.StageID || got.ItemID != b.ItemID || len(got.ArtifactVersionIDs) != 0 {
			t.Fatalf("%s context %+v: %v", kind, got, err)
		}
	}
	if got, err := d.Store.GetCreativeSessionContext("worker"); err != nil || !reflect.DeepEqual(got, b) {
		t.Fatalf("session context %+v: %v", got, err)
	}
	versions, err := d.Store.ListCreativeArtifactVersions(pub.Artifact.ID)
	if err != nil || len(versions) != 1 || versions[0].ID != pub.Version.ID {
		t.Fatalf("versions %+v: %v", versions, err)
	}
	content := artifactRequest(h, "GET", pub.Version.ContentURL, "", "", nil)
	if content.Code != 200 || content.Body.String() != "published result" {
		t.Fatalf("artifact %d: %s", content.Code, content.Body.String())
	}
	after, err := d.Store.GetCreativeRun(run.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("close changed pipeline: %v", err)
	}
}

func TestCreativeCloseAlreadyTerminalRetainsState(t *testing.T) {
	for _, state := range []string{"killed", "done", "errored"} {
		t.Run(state, func(t *testing.T) {
			d, run, rt, ws := creativeCloseFixture(t)
			if err := d.Store.UpdateSessionState("worker", state); err != nil {
				t.Fatal(err)
			}
			w := artifactRequest(NewHandler(d), http.MethodPost, fmt.Sprintf("/v1/creative/runs/%d/workers/worker/close", run.ID), "", "orch", nil)
			s, err := d.Store.GetSession("worker")
			if w.Code != 200 || err != nil || s.State != state || len(rt.stopped) != 0 || len(ws.deleted) != 0 {
				t.Fatalf("status=%d state=%s stops=%v cleanup=%v err=%v", w.Code, s.State, rt.stopped, ws.deleted, err)
			}
		})
	}
}
