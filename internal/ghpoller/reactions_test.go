package ghpoller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
)

// --- fakes for Manager dependencies -----------------------------------

type reactFakeRuntime struct {
	mu        sync.Mutex
	destroyed []string
}

func (f *reactFakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (f *reactFakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string) error {
	return nil
}
func (f *reactFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (f *reactFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (f *reactFakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, h.Name)
	return nil
}
func (f *reactFakeRuntime) AttachCommand(h runtime.Handle) []string    { return nil }
func (f *reactFakeRuntime) List(ctx context.Context) ([]string, error) { return nil, nil }

type reactFakeWorkspace struct {
	mu        sync.Mutex
	destroyed []string
}

func (f *reactFakeWorkspace) Create(ctx context.Context, repo store.Repo, sessionID, branch string) (workspace.CreateResult, error) {
	return workspace.CreateResult{Path: "/fake/wt/" + sessionID}, nil
}
func (f *reactFakeWorkspace) Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error) {
	return "/fake/wt/" + sessionID, nil
}
func (f *reactFakeWorkspace) Destroy(ctx context.Context, repo store.Repo, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, sessionID)
	return nil
}

func (f *reactFakeWorkspace) destroyedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.destroyed)
}

// --- test scaffolding ---------------------------------------------------

// reactEnv bundles everything a reactions test needs: a store with a repo,
// project, worker session and (optionally) a subtask task row, plus a real
// session.Manager wired to fake runtime/workspace so Complete's effects are
// observable.
type reactEnv struct {
	st  *store.Store
	b   *bus.Bus
	rt  *reactFakeRuntime
	ws  *reactFakeWorkspace
	mgr *session.Manager
	cfg *config.Config
}

func setupReactEnv(t *testing.T, grace time.Duration, autoCleanup bool) *reactEnv {
	t.Helper()

	dir := t.TempDir()
	st, err := store.Open(dir + "/rocket.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.AddRepo(store.Repo{ID: "api", Path: dir + "/repo", AutoCleanup: autoCleanup}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "billing", Name: "Billing", MainRepo: "api"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	b := bus.New(st)
	rt := &reactFakeRuntime{}
	ws := &reactFakeWorkspace{}
	cfg := &config.Config{Home: dir, DefaultAgent: "fake", MergeGrace: grace}
	mgr := session.NewManager(st, b, rt, ws, cfg)

	return &reactEnv{st: st, b: b, rt: rt, ws: ws, mgr: mgr, cfg: cfg}
}

func (e *reactEnv) addWorker(t *testing.T, id string) store.Session {
	t.Helper()
	sess := store.Session{
		ID: id, Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "fake", Branch: "feature/f1/" + id,
		WorktreePath: "/wt/" + id, TmuxName: id, State: "running",
	}
	if err := e.st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	return sess
}

func (e *reactEnv) addSubtask(t *testing.T, sessionID, status string) store.Task {
	t.Helper()
	id, err := e.st.AddTask(store.Task{
		Title: "subtask", ProjectID: "billing", RepoID: "api",
		Status: status, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	task, err := e.st.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return task
}

// alwaysUnknownActivity reports "not known" for every session, matching
// what most tests want (grace fires immediately, worker considered idle).
func alwaysUnknownActivity(string) (activity.State, bool) { return "", false }

// idleActivity reports Idle (known) for every session.
func idleActivity(string) (activity.State, bool) { return activity.Idle, true }

func newTestReactions(e *reactEnv, wake func(string), getActivity func(string) (activity.State, bool)) *Reactions {
	return NewReactions(e.st, e.b, wake, e.mgr, getActivity, e.cfg)
}

func fakePR(number int, sha string) *github.PR {
	return &github.PR{Number: number, HeadSHA: sha, State: "open"}
}

// --- PROpened -------------------------------------------------------------

func TestPROpened_TransitionsSubtaskToReview(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")
	task := e.addSubtask(t, sess.ID, "in_progress")

	r := newTestReactions(e, func(string) {}, alwaysUnknownActivity)
	r.PROpened(sess, fakePR(5, "sha1"))

	got, err := e.st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "review" {
		t.Fatalf("status = %q, want review", got.Status)
	}

	logs, err := e.st.ListTaskLog(task.ID, "")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d: %+v", len(logs), logs)
	}
}

func TestPROpened_NoSubtask_NoOp(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")

	r := newTestReactions(e, func(string) {}, alwaysUnknownActivity)
	// Must not panic when no task references this session.
	r.PROpened(sess, fakePR(5, "sha1"))
}

// --- CIFailing dedup --------------------------------------------------

func TestCIFailing_DedupPerSHA(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")

	var woken []string
	r := newTestReactions(e, func(to string) { woken = append(woken, to) }, alwaysUnknownActivity)

	r.CIFailing(sess, fakePR(5, "sha1"), "checks failing")
	r.CIFailing(sess, fakePR(5, "sha1"), "checks failing")

	msgs := queuedMessagesFor(t, e.st, sess.ID)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 queued message after duplicate SHA calls, got %d: %+v", len(msgs), msgs)
	}
	if len(woken) != 1 {
		t.Fatalf("expected wake called once, got %d", len(woken))
	}

	r.CIFailing(sess, fakePR(5, "sha2"), "checks failing")
	msgs = queuedMessagesFor(t, e.st, sess.ID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 queued messages after new SHA, got %d: %+v", len(msgs), msgs)
	}

	want := "[rocket] CI failing on PR #5: checks failing. Investigate and fix."
	if msgs[0].Body != want {
		t.Errorf("message body = %q, want %q", msgs[0].Body, want)
	}
}

func TestCIFailing_SkipsTerminalSession(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")

	// Kill the session first.
	if err := e.mgr.Kill(context.Background(), sess.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	r := newTestReactions(e, func(string) {}, alwaysUnknownActivity)

	// CIFailing on a killed session should not queue any message.
	r.CIFailing(sess, fakePR(5, "sha1"), "checks failing")

	msgs := queuedMessagesFor(t, e.st, sess.ID)
	if len(msgs) != 0 {
		t.Fatalf("expected no queued messages for terminal session, got %d: %+v", len(msgs), msgs)
	}
}

// --- ChangesRequested ----------------------------------------------------

func TestChangesRequested_MessageContent(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")

	r := newTestReactions(e, func(string) {}, alwaysUnknownActivity)
	r.ChangesRequested(sess, fakePR(5, "sha1"))

	msgs := queuedMessagesFor(t, e.st, sess.ID)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 queued message, got %d", len(msgs))
	}
	want := "[rocket] Changes requested on PR #5. Address the review comments."
	if msgs[0].Body != want {
		t.Errorf("message body = %q, want %q", msgs[0].Body, want)
	}
}

// --- Merged: subtask transition -----------------------------------------

func TestMerged_SubtaskToDone(t *testing.T) {
	e := setupReactEnv(t, time.Minute, true)
	sess := e.addWorker(t, "w1")
	task := e.addSubtask(t, sess.ID, "review")

	r := newTestReactions(e, func(string) {}, alwaysUnknownActivity)
	r.Merged(sess, fakePR(5, "sha1"))

	got, err := e.st.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("status = %q, want done", got.Status)
	}

	logs, err := e.st.ListTaskLog(task.ID, "")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
}

// --- Merged: grace-timer cleanup -----------------------------------------

func TestMergedGrace_WorkerIdle_CompletesAfterGrace(t *testing.T) {
	grace := 30 * time.Millisecond
	e := setupReactEnv(t, grace, true)
	sess := e.addWorker(t, "w1")

	ch, cancel := e.b.Subscribe()
	defer cancel()

	r := newTestReactions(e, func(string) {}, idleActivity)
	defer r.Stop()

	r.Merged(sess, fakePR(5, "sha1"))

	waitForSessionState(t, e.st, sess.ID, "done", time.Second)

	if e.ws.destroyedCount() != 1 {
		t.Fatalf("expected workspace destroyed once, got %d", e.ws.destroyedCount())
	}

	var sawCleanup bool
	deadline := time.After(time.Second)
	for !sawCleanup {
		select {
		case ev := <-ch:
			if ev.Type == "workspace.cleanup" {
				sawCleanup = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for workspace.cleanup event")
		}
	}
}

func TestMergedGrace_WorkerActive_ReschedulesUntilIdle(t *testing.T) {
	grace := 20 * time.Millisecond
	e := setupReactEnv(t, grace, true)
	sess := e.addWorker(t, "w1")

	var active atomicBool
	active.set(true)
	getActivity := func(string) (activity.State, bool) {
		if active.get() {
			return activity.Active, true
		}
		return activity.Idle, true
	}

	r := newTestReactions(e, func(string) {}, getActivity)
	defer r.Stop()

	r.Merged(sess, fakePR(5, "sha1"))

	// Let at least one grace window elapse while the worker is still
	// "active": Complete must not have run yet.
	time.Sleep(3 * grace)
	got, err := e.st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.State != "running" {
		t.Fatalf("expected session still running while active, got %q", got.State)
	}

	// Flip to idle; the next grace window should complete it.
	active.set(false)
	waitForSessionState(t, e.st, sess.ID, "done", time.Second)
}

func TestMergedGrace_AutoCleanupFalse_Untouched(t *testing.T) {
	grace := 20 * time.Millisecond
	e := setupReactEnv(t, grace, false)
	sess := e.addWorker(t, "w1")

	r := newTestReactions(e, func(string) {}, idleActivity)
	defer r.Stop()

	r.Merged(sess, fakePR(5, "sha1"))

	time.Sleep(5 * grace)
	got, err := e.st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.State != "running" {
		t.Fatalf("expected session untouched (AutoCleanup=false), got state %q", got.State)
	}
	if e.ws.destroyedCount() != 0 {
		t.Fatalf("expected no workspace destroy, got %d", e.ws.destroyedCount())
	}
}

func TestMergedGrace_SessionAlreadyKilled_Untouched(t *testing.T) {
	grace := 20 * time.Millisecond
	e := setupReactEnv(t, grace, true)
	sess := e.addWorker(t, "w1")

	if err := e.mgr.Kill(context.Background(), sess.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	r := newTestReactions(e, func(string) {}, idleActivity)
	defer r.Stop()

	r.Merged(sess, fakePR(5, "sha1"))

	time.Sleep(5 * grace)
	got, err := e.st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.State != "killed" {
		t.Fatalf("expected session to remain killed, got %q", got.State)
	}
	if e.ws.destroyedCount() != 0 {
		t.Fatalf("expected no workspace destroy for an already-killed session, got %d", e.ws.destroyedCount())
	}
}

// --- Stop -----------------------------------------------------------------

func TestStop_CancelsPendingTimers(t *testing.T) {
	grace := 20 * time.Millisecond
	e := setupReactEnv(t, grace, true)
	sess := e.addWorker(t, "w1")

	r := newTestReactions(e, func(string) {}, idleActivity)
	r.Merged(sess, fakePR(5, "sha1"))
	r.Stop()

	time.Sleep(5 * grace)
	got, err := e.st.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.State != "running" {
		t.Fatalf("expected session untouched after Stop(), got %q", got.State)
	}
}

// --- helpers ---------------------------------------------------------------

func queuedMessagesFor(t *testing.T, st *store.Store, to string) []store.Message {
	t.Helper()
	msgs, err := st.ListMessages(to, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	return msgs
}

func waitForSessionState(t *testing.T, st *store.Store, id, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, err := st.GetSession(id)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %q to reach state %q", id, want)
}

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBool) set(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

func (a *atomicBool) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}
