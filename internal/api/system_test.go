package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
)

// systemFakeRuntime is a runtime.Runtime fake with a configurable List()
// result and a record of Destroy calls, for exercising /v1/system's tmux
// inspection and cleanup.
type systemFakeRuntime struct {
	names     []string
	destroyed []string
}

func (f *systemFakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (f *systemFakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	return nil
}
func (f *systemFakeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	return nil
}
func (f *systemFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (f *systemFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (f *systemFakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (f *systemFakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }

func (f *systemFakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error {
	f.destroyed = append(f.destroyed, h.Name)
	return nil
}
func (f *systemFakeRuntime) AttachCommand(h runtime.Handle) []string {
	return []string{"tmux", "attach", "-t", h.Name}
}
func (f *systemFakeRuntime) List(ctx context.Context) ([]string, error) {
	return f.names, nil
}

// systemTestDeps builds Deps with a real store/bus/manager on a temp dir,
// wired to a systemFakeRuntime and a real (on-disk) workspace, for tests
// exercising /v1/system and /v1/system/cleanup.
func systemTestDeps(t *testing.T, rt *systemFakeRuntime) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	worktreesDir := filepath.Join(dir, "worktrees")
	cfg := &config.Config{
		Home:         dir,
		Port:         4477,
		WorktreesDir: worktreesDir,
	}
	mgr := session.NewManager(st, b, rt, workspace.New(worktreesDir), cfg)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Manager = mgr
	d.StartedAt = time.Now().Add(-10 * time.Second)
	return d, dir
}

func TestGetSystemQueueCounts(t *testing.T) {
	d, _ := systemTestDeps(t, &systemFakeRuntime{})
	srv := newTestServer(t, d)

	for i := 0; i < 2; i++ {
		if _, err := d.Store.AddMessage(store.Message{ToSession: "s1", Body: "hi", Status: "queued"}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	if _, err := d.Store.AddMessage(store.Message{ToSession: "s1", Body: "hi", Status: "failed"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Queue.Queued != 2 {
		t.Errorf("queue.queued = %d, want 2", body.Queue.Queued)
	}
	if body.Queue.Failed != 1 {
		t.Errorf("queue.failed = %d, want 1", body.Queue.Failed)
	}
	if body.Daemon.Version == "" {
		t.Errorf("daemon.version is empty")
	}
	if body.Daemon.Port != 4477 {
		t.Errorf("daemon.port = %d, want 4477", body.Daemon.Port)
	}
	if body.Daemon.UptimeS < 10 {
		t.Errorf("daemon.uptime_s = %d, want >= 10", body.Daemon.UptimeS)
	}
}

func TestGetSystemOrphanTmux(t *testing.T) {
	rt := &systemFakeRuntime{names: []string{"live-sess", "orphan-sess"}}
	d, dir := systemTestDeps(t, rt)
	srv := newTestServer(t, d)

	seedSystemSession(t, d.Store, dir, "live-sess")

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	byName := map[string]tmuxResponse{}
	for _, tm := range body.Tmux {
		byName[tm.Name] = tm
	}

	live, ok := byName["live-sess"]
	if !ok {
		t.Fatalf("live-sess missing from tmux list")
	}
	if live.Orphan {
		t.Errorf("live-sess should not be orphan")
	}
	if live.SessionID != "live-sess" {
		t.Errorf("live-sess session_id = %q, want live-sess", live.SessionID)
	}

	orphan, ok := byName["orphan-sess"]
	if !ok {
		t.Fatalf("orphan-sess missing from tmux list")
	}
	if !orphan.Orphan {
		t.Errorf("orphan-sess should be orphan")
	}
	if orphan.SessionID != "" {
		t.Errorf("orphan-sess session_id = %q, want empty", orphan.SessionID)
	}
}

func TestGetSystemLogTailCapped(t *testing.T) {
	d, dir := systemTestDeps(t, &systemFakeRuntime{})
	srv := newTestServer(t, d)

	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	logPath := filepath.Join(logsDir, "rocketd.log")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}
	for i := 0; i < 500; i++ {
		if _, err := f.WriteString("line\n"); err != nil {
			t.Fatalf("write log line: %v", err)
		}
	}
	f.Close()

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.LogTail) != 200 {
		t.Errorf("len(log_tail) = %d, want 200", len(body.LogTail))
	}
}

func TestPostSystemCleanupOnlyTouchesOrphans(t *testing.T) {
	rt := &systemFakeRuntime{names: []string{"live-sess", "orphan-sess"}}
	d, dir := systemTestDeps(t, rt)
	srv := newTestServer(t, d)

	seedSystemSession(t, d.Store, dir, "live-sess")

	// Live worktree (referenced by live-sess) and an orphan worktree with
	// no store session at all.
	liveWtPath := filepath.Join(dir, "worktrees", "repo1", "live-sess")
	if err := os.MkdirAll(liveWtPath, 0755); err != nil {
		t.Fatalf("mkdir live worktree: %v", err)
	}
	orphanWtPath := filepath.Join(dir, "worktrees", "repo1", "orphan-wt")
	if err := os.MkdirAll(orphanWtPath, 0755); err != nil {
		t.Fatalf("mkdir orphan worktree: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/system/cleanup", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		KilledTmux       []string `json:"killed_tmux"`
		RemovedWorktrees []string `json:"removed_worktrees"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if len(body.KilledTmux) != 1 || body.KilledTmux[0] != "orphan-sess" {
		t.Errorf("killed_tmux = %v, want [orphan-sess]", body.KilledTmux)
	}
	if len(rt.destroyed) != 1 || rt.destroyed[0] != "orphan-sess" {
		t.Errorf("runtime destroyed = %v, want [orphan-sess]", rt.destroyed)
	}

	if len(body.RemovedWorktrees) != 1 || body.RemovedWorktrees[0] != orphanWtPath {
		t.Errorf("removed_worktrees = %v, want [%s]", body.RemovedWorktrees, orphanWtPath)
	}

	// The live session's tmux name and worktree dir must be untouched.
	if _, err := os.Stat(liveWtPath); err != nil {
		t.Errorf("live worktree dir was removed: %v", err)
	}
	if _, err := os.Stat(orphanWtPath); !os.IsNotExist(err) {
		t.Errorf("orphan worktree dir still present, err = %v", err)
	}
}

// TestGetSystemKilledSessionResourcesNotOrphan verifies that a killed
// session's leftover tmux name and worktree directory are reported with
// orphan:false and state:"killed" (not treated as orphans just because
// ListSessions' default filter excludes terminal states), and that cleanup
// leaves them untouched.
func TestGetSystemKilledSessionResourcesNotOrphan(t *testing.T) {
	rt := &systemFakeRuntime{names: []string{"killed-sess"}}
	d, dir := systemTestDeps(t, rt)
	srv := newTestServer(t, d)

	seedSystemSessionState(t, d.Store, dir, "killed-sess", "killed")

	killedWtPath := filepath.Join(dir, "worktrees", "repo1", "killed-sess")
	if err := os.MkdirAll(killedWtPath, 0755); err != nil {
		t.Fatalf("mkdir killed worktree: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	tmByName := map[string]tmuxResponse{}
	for _, tm := range body.Tmux {
		tmByName[tm.Name] = tm
	}
	killedTm, ok := tmByName["killed-sess"]
	if !ok {
		t.Fatalf("killed-sess missing from tmux list")
	}
	if killedTm.Orphan {
		t.Errorf("killed-sess tmux should not be orphan (has a store record)")
	}
	if killedTm.SessionID != "killed-sess" {
		t.Errorf("killed-sess tmux session_id = %q, want killed-sess", killedTm.SessionID)
	}
	if killedTm.State != "killed" {
		t.Errorf("killed-sess tmux state = %q, want killed", killedTm.State)
	}

	wtByPath := map[string]worktreeResponse{}
	for _, wt := range body.Worktrees {
		wtByPath[wt.Path] = wt
	}
	killedWt, ok := wtByPath[killedWtPath]
	if !ok {
		t.Fatalf("killed worktree missing from worktrees list")
	}
	if killedWt.Orphan {
		t.Errorf("killed worktree should not be orphan (has a store record)")
	}
	if killedWt.State != "killed" {
		t.Errorf("killed worktree state = %q, want killed", killedWt.State)
	}

	// Cleanup must not touch either resource: they belong to a killed
	// session, not a true orphan.
	cResp := postJSON(t, srv.URL+"/v1/system/cleanup", map[string]any{})
	defer cResp.Body.Close()
	if cResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", cResp.StatusCode)
	}
	var cBody struct {
		KilledTmux       []string `json:"killed_tmux"`
		RemovedWorktrees []string `json:"removed_worktrees"`
	}
	if err := json.NewDecoder(cResp.Body).Decode(&cBody); err != nil {
		t.Fatalf("decode cleanup body: %v", err)
	}
	if len(cBody.KilledTmux) != 0 {
		t.Errorf("killed_tmux = %v, want empty (killed session's tmux is not an orphan)", cBody.KilledTmux)
	}
	if len(cBody.RemovedWorktrees) != 0 {
		t.Errorf("removed_worktrees = %v, want empty (killed session's worktree is not an orphan)", cBody.RemovedWorktrees)
	}
	if len(rt.destroyed) != 0 {
		t.Errorf("runtime destroyed = %v, want empty", rt.destroyed)
	}
	if _, err := os.Stat(killedWtPath); err != nil {
		t.Errorf("killed worktree dir was removed: %v", err)
	}
}

// TestGetSystemTrueOrphanHasNoState verifies that a resource with no store
// record at all (a true orphan) is reported with orphan:true and an empty
// state, and IS removed by cleanup.
func TestGetSystemTrueOrphanHasNoState(t *testing.T) {
	rt := &systemFakeRuntime{names: []string{"orphan-sess"}}
	d, dir := systemTestDeps(t, rt)
	srv := newTestServer(t, d)

	orphanWtPath := filepath.Join(dir, "worktrees", "repo1", "orphan-wt")
	if err := os.MkdirAll(orphanWtPath, 0755); err != nil {
		t.Fatalf("mkdir orphan worktree: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	tmByName := map[string]tmuxResponse{}
	for _, tm := range body.Tmux {
		tmByName[tm.Name] = tm
	}
	orphanTm, ok := tmByName["orphan-sess"]
	if !ok {
		t.Fatalf("orphan-sess missing from tmux list")
	}
	if !orphanTm.Orphan {
		t.Errorf("orphan-sess tmux should be orphan (no store record)")
	}
	if orphanTm.State != "" {
		t.Errorf("orphan-sess tmux state = %q, want empty", orphanTm.State)
	}

	wtByPath := map[string]worktreeResponse{}
	for _, wt := range body.Worktrees {
		wtByPath[wt.Path] = wt
	}
	orphanWt, ok := wtByPath[orphanWtPath]
	if !ok {
		t.Fatalf("orphan worktree missing from worktrees list")
	}
	if !orphanWt.Orphan {
		t.Errorf("orphan worktree should be orphan (no store record)")
	}
	if orphanWt.State != "" {
		t.Errorf("orphan worktree state = %q, want empty", orphanWt.State)
	}

	cResp := postJSON(t, srv.URL+"/v1/system/cleanup", map[string]any{})
	defer cResp.Body.Close()
	var cBody struct {
		KilledTmux       []string `json:"killed_tmux"`
		RemovedWorktrees []string `json:"removed_worktrees"`
	}
	if err := json.NewDecoder(cResp.Body).Decode(&cBody); err != nil {
		t.Fatalf("decode cleanup body: %v", err)
	}
	if len(cBody.KilledTmux) != 1 || cBody.KilledTmux[0] != "orphan-sess" {
		t.Errorf("killed_tmux = %v, want [orphan-sess]", cBody.KilledTmux)
	}
	if len(cBody.RemovedWorktrees) != 1 || cBody.RemovedWorktrees[0] != orphanWtPath {
		t.Errorf("removed_worktrees = %v, want [%s]", cBody.RemovedWorktrees, orphanWtPath)
	}
	if _, err := os.Stat(orphanWtPath); !os.IsNotExist(err) {
		t.Errorf("orphan worktree dir still present, err = %v", err)
	}
}

// TestGetSystemSurvivesUnreadableWorktreeEntry verifies that an unreadable
// file inside a worktree directory (e.g. permission denied) doesn't fail
// GET /v1/system; dirSize should skip the unreadable entry and keep
// walking, and handleGetSystem should still return 200 with the rest of the
// worktree data intact.
func TestGetSystemSurvivesUnreadableWorktreeEntry(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission bits don't block access")
	}

	d, dir := systemTestDeps(t, &systemFakeRuntime{})
	srv := newTestServer(t, d)

	wtPath := filepath.Join(dir, "worktrees", "repo1", "sess-with-bad-file")
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	readableFile := filepath.Join(wtPath, "readable.txt")
	if err := os.WriteFile(readableFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("write readable file: %v", err)
	}
	unreadableDir := filepath.Join(wtPath, "noperm")
	if err := os.MkdirAll(unreadableDir, 0755); err != nil {
		t.Fatalf("mkdir noperm dir: %v", err)
	}
	unreadableFile := filepath.Join(unreadableDir, "secret.txt")
	if err := os.WriteFile(unreadableFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write unreadable file: %v", err)
	}
	if err := os.Chmod(unreadableDir, 0000); err != nil {
		t.Fatalf("chmod noperm dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(unreadableDir, 0755) })

	resp, err := http.Get(srv.URL + "/v1/system")
	if err != nil {
		t.Fatalf("GET /v1/system: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a single unreadable entry must not fail the whole request)", resp.StatusCode)
	}

	var body systemResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	found := false
	for _, wt := range body.Worktrees {
		if wt.Path == wtPath {
			found = true
			if wt.SizeBytes < int64(len("hello")) {
				t.Errorf("size_bytes = %d, want at least %d (readable.txt counted)", wt.SizeBytes, len("hello"))
			}
		}
	}
	if !found {
		t.Errorf("worktree %s missing from response despite unreadable entry inside it", wtPath)
	}
}

// seedSystemSession inserts a project/repo/session triple whose session id,
// TmuxName and worktree path are all sessID, so that the tmux name sessID
// is considered "live" (referenced by a running session). worktreesRootDir
// is the systemTestDeps home dir; the worktree path is built to match the
// real on-disk layout under <home>/worktrees/repo1/<sessID>, so it lines up
// with what workspace.List() reports for a directory created at that path.
func seedSystemSession(t *testing.T, st *store.Store, worktreesRootDir, sessID string) {
	t.Helper()
	seedSystemSessionState(t, st, worktreesRootDir, sessID, "running")
}

// seedSystemSessionState is seedSystemSession with an explicit session
// state, for exercising resources owned by killed/errored/done sessions.
func seedSystemSessionState(t *testing.T, st *store.Store, worktreesRootDir, sessID, state string) {
	t.Helper()
	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "proj1", MainRepo: "repo1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := st.AddSession(store.Session{
		ID:           sessID,
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  sessID,
		Agent:        "fake",
		Branch:       "feature/" + sessID,
		WorktreePath: filepath.Join(worktreesRootDir, "worktrees", "repo1", sessID),
		TmuxName:     sessID,
		State:        state,
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}
