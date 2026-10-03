package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

type fakeUsageCollector struct {
	mu        sync.Mutex
	snapshots []string
	enqueued  []string
	terminal  [][2]bool
	queuedN   int
}

func (f *fakeUsageCollector) Snapshot(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append(f.snapshots, id)
}

func (f *fakeUsageCollector) Enqueue(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, id)
}

func (f *fakeUsageCollector) EnqueueTerminal(collected, missing bool) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminal = append(f.terminal, [2]bool{collected, missing})
	return f.queuedN, nil
}

// newUsageFixture is the model-profile fixture (orch/wrk/cto sessions, a
// feature with one subtask) served with a fake usage collector.
func newUsageFixture(t *testing.T) (mpFixture, *fakeUsageCollector) {
	t.Helper()
	f := newMPFixture(t)
	fc := &fakeUsageCollector{queuedN: 7}
	f.d.Bus = bus.New(f.d.Store) // PATCH status publishes task.status_changed
	f.d.Usage = fc
	srv := httptest.NewServer(NewHandler(f.d))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f, fc
}

func TestCollectUsageSession(t *testing.T) {
	f, fc := newUsageFixture(t)
	status, body := mpDo(t, f.url, "POST", "/v1/stats/usage/collect", "", map[string]any{"session_id": "wrk"})
	if status != http.StatusAccepted || body["queued"] != float64(1) {
		t.Fatalf("collect = %d %v", status, body)
	}
	if len(fc.enqueued) != 1 || fc.enqueued[0] != "wrk" {
		t.Fatalf("enqueued = %v", fc.enqueued)
	}
	status, body = mpDo(t, f.url, "POST", "/v1/stats/usage/collect", "", map[string]any{"session_id": "nope"})
	if status != http.StatusNotFound || errCode(body) != "session_not_found" {
		t.Fatalf("unknown session = %d %v", status, body)
	}
}

func TestCollectUsageBulk(t *testing.T) {
	f, fc := newUsageFixture(t)
	for _, tc := range []struct {
		body map[string]any
		want [2]bool
	}{
		{map[string]any{"all": true}, [2]bool{true, false}},
		{map[string]any{"all": true, "retry_missing": true}, [2]bool{true, true}},
		{map[string]any{"retry_missing": true}, [2]bool{false, true}},
	} {
		fc.terminal = nil
		status, body := mpDo(t, f.url, "POST", "/v1/stats/usage/collect", "", tc.body)
		if status != http.StatusAccepted || body["queued"] != float64(7) {
			t.Fatalf("%v = %d %v", tc.body, status, body)
		}
		if len(fc.terminal) != 1 || fc.terminal[0] != tc.want {
			t.Fatalf("%v -> %v, want %v", tc.body, fc.terminal, tc.want)
		}
	}
}

func TestCollectUsageBadRequests(t *testing.T) {
	f, _ := newUsageFixture(t)
	for _, body := range []any{
		map[string]any{},
		map[string]any{"session_id": "wrk", "all": true},
		"not an object",
	} {
		status, resp := mpDo(t, f.url, "POST", "/v1/stats/usage/collect", "", body)
		if status != http.StatusBadRequest || errCode(resp) != "bad_request" {
			t.Errorf("%v = %d %v", body, status, resp)
		}
	}
}

func TestCollectUsageIsHumanOnly(t *testing.T) {
	f, fc := newUsageFixture(t)
	for _, caller := range []string{"orch", "wrk", "cto"} {
		status, body := mpDo(t, f.url, "POST", "/v1/stats/usage/collect", caller, map[string]any{"all": true})
		if status != http.StatusForbidden || errCode(body) != "human_only" {
			t.Errorf("as %s = %d %v", caller, status, body)
		}
	}
	if len(fc.terminal) != 0 {
		t.Fatalf("agent queued a collection: %v", fc.terminal)
	}
}

func TestCollectUsageWithoutCollector(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "POST", "/v1/stats/usage/collect", "", map[string]any{"all": true})
	if status != http.StatusServiceUnavailable || errCode(body) != "unavailable" {
		t.Fatalf("no collector = %d %v", status, body)
	}
}

func TestRootTaskReviewSnapshotsOrchestrator(t *testing.T) {
	f, fc := newUsageFixture(t)
	// Blocked review (live worker, open subtask): no snapshot.
	if status, _ := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(f.featureID), "", map[string]any{"status": "review"}); status != http.StatusConflict {
		t.Fatalf("blocked review status = %d", status)
	}
	// A subtask going to review is not a feature review.
	if status, _ := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(f.subID), "", map[string]any{"status": "review"}); status != http.StatusOK {
		t.Fatalf("subtask review status = %d", status)
	}
	// Another status on the root task.
	if status, _ := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(f.featureID), "", map[string]any{"status": "backlog"}); status != http.StatusOK {
		t.Fatalf("backlog status = %d", status)
	}
	if len(fc.snapshots) != 0 {
		t.Fatalf("unexpected snapshots: %v", fc.snapshots)
	}
	status, _ := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(f.featureID)+"?force=true", "", map[string]any{"status": "review"})
	if status != http.StatusOK {
		t.Fatalf("review status = %d", status)
	}
	if len(fc.snapshots) != 1 || fc.snapshots[0] != "orch" {
		t.Fatalf("snapshots = %v", fc.snapshots)
	}
}

func TestRootTaskReviewWithoutOrchestratorOrCollector(t *testing.T) {
	f, fc := newUsageFixture(t)
	id, err := f.d.Store.AddTask(store.Task{Title: "no orch", ProjectID: "p", Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := mpDo(t, f.url, "PATCH", "/v1/tasks/"+itoa(id), "", map[string]any{"status": "review"}); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if len(fc.snapshots) != 0 {
		t.Fatalf("snapshots = %v", fc.snapshots)
	}
	plain := newMPFixture(t) // Deps.Usage == nil must not break the PATCH
	plain.d.Bus = bus.New(plain.d.Store)
	plain.url = newTestServer(t, plain.d).URL
	if status, _ := mpDo(t, plain.url, "PATCH", "/v1/tasks/"+itoa(plain.featureID)+"?force=true", "", map[string]any{"status": "review"}); status != http.StatusOK {
		t.Fatalf("nil collector review = %d", status)
	}
}
