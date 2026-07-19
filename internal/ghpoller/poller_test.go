package ghpoller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeNotifier records PRNotifier calls for assertions.
type fakeNotifier struct {
	mu               sync.Mutex
	opened           []int
	ciFailing        []int
	changesRequested []int
	merged           []int
}

func (f *fakeNotifier) PROpened(sess store.Session, pr *github.PR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, pr.Number)
}
func (f *fakeNotifier) CIFailing(sess store.Session, pr *github.PR, summary string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ciFailing = append(f.ciFailing, pr.Number)
}
func (f *fakeNotifier) ChangesRequested(sess store.Session, pr *github.PR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changesRequested = append(f.changesRequested, pr.Number)
}
func (f *fakeNotifier) Merged(sess store.Session, pr *github.PR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged = append(f.merged, pr.Number)
}

func (f *fakeNotifier) counts() (opened, ciFailing, changesRequested, merged int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.opened), len(f.ciFailing), len(f.changesRequested), len(f.merged)
}

// setupEnv creates a store with a repo whose local path is a temp git repo
// with a github.com origin remote, a project, and a running worker session
// on that repo. Returns the store, bus and session id.
func setupEnv(t *testing.T) (*store.Store, *bus.Bus) {
	t.Helper()

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	runGit(t, "init", repoDir)
	runGit(t, "-C", repoDir, "remote", "add", "origin", "git@github.com:o/r.git")

	dbPath := filepath.Join(dir, "rocket.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.AddRepo(store.Repo{ID: "api", Path: repoDir}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "billing", Name: "Billing", MainRepo: "api"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	b := bus.New(st)
	return st, b
}

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func addWorker(t *testing.T, st *store.Store, id, branch string) store.Session {
	t.Helper()
	sess := store.Session{
		ID: id, Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: branch,
		WorktreePath: "/wt/" + id, TmuxName: "t-" + id, State: "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	return sess
}

func testConfig() *config.Config {
	return &config.Config{GithubPollInterval: time.Minute}
}

func ghFactory(url string) func() (*github.Client, error) {
	return func() (*github.Client, error) {
		return github.New(url, "tok"), nil
	}
}

func TestTick_ErrNoToken_NoOp(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	gh := func() (*github.Client, error) { return nil, github.ErrNoToken }
	notify := &fakeNotifier{}
	p := New(st, b, gh, testConfig(), notify)

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRNumber != 0 || got.PRState != "" {
		t.Fatalf("expected no PR fields set, got %+v", got)
	}
	if opened, _, _, _ := notify.counts(); opened != 0 {
		t.Fatalf("expected no notifications, got opened=%d", opened)
	}
}

// mockGitHubServer is a small stateful stub of the GitHub REST endpoints
// this package uses: pulls list-by-head, pulls get, pulls reviews, and
// commit check-runs.
type mockGitHubServer struct {
	mu sync.Mutex

	prNumber int
	prState  string // "open" or "closed"
	merged   bool
	headSHA  string

	checkConclusion string // "" (no runs => passing), "success", "failure"
	reviews         []reviewStub
}

type reviewStub struct {
	login       string
	state       string
	submittedAt string
}

func newMockGitHubServer() *mockGitHubServer {
	return &mockGitHubServer{prNumber: 5, prState: "open", headSHA: "sha1"}
}

func (m *mockGitHubServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch {
		case r.URL.Path == "/repos/o/r/pulls":
			w.Write([]byte(`[{"number":` + itoa(m.prNumber) + `,"state":"` + m.prState + `","merged":` + boolStr(m.merged) + `,"head":{"sha":"` + m.headSHA + `"}}]`))
		case r.URL.Path == "/repos/o/r/pulls/5":
			w.Write([]byte(`{"number":` + itoa(m.prNumber) + `,"state":"` + m.prState + `","merged":` + boolStr(m.merged) + `,"head":{"sha":"` + m.headSHA + `"}}`))
		case r.URL.Path == "/repos/o/r/pulls/5/reviews":
			w.Write([]byte(m.reviewsJSON()))
		case r.URL.Path == "/repos/o/r/commits/"+m.headSHA+"/check-runs":
			w.Write([]byte(m.checkRunsJSON()))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (m *mockGitHubServer) reviewsJSON() string {
	if len(m.reviews) == 0 {
		return `[]`
	}
	s := `[`
	for i, rv := range m.reviews {
		if i > 0 {
			s += `,`
		}
		s += `{"user":{"login":"` + rv.login + `"},"state":"` + rv.state + `","submitted_at":"` + rv.submittedAt + `"}`
	}
	s += `]`
	return s
}

func (m *mockGitHubServer) checkRunsJSON() string {
	if m.checkConclusion == "" {
		return `{"total_count":0,"check_runs":[]}`
	}
	return `{"total_count":1,"check_runs":[{"status":"completed","conclusion":"` + m.checkConclusion + `"}]}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestTick_Discovery(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	m := newMockGitHubServer()
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	notify := &fakeNotifier{}
	p := New(st, b, ghFactory(srv.URL), testConfig(), notify)

	sub, cancel := b.Subscribe()
	defer cancel()

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRNumber != 5 || got.PRState != "open" || got.CIState != "passing" {
		t.Fatalf("unexpected session PR fields: %+v", got)
	}

	if opened, _, _, _ := notify.counts(); opened != 1 {
		t.Fatalf("expected 1 PROpened call, got %d", opened)
	}

	select {
	case e := <-sub:
		if e.Type != "pr.opened" {
			t.Fatalf("expected pr.opened event, got %s", e.Type)
		}
	default:
		t.Fatal("expected pr.opened event on bus")
	}
}

func TestTick_CIChange(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	m := newMockGitHubServer()
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	notify := &fakeNotifier{}
	p := New(st, b, ghFactory(srv.URL), testConfig(), notify)

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}

	m.mu.Lock()
	m.checkConclusion = "failure"
	m.mu.Unlock()

	sub, cancel := b.Subscribe()
	defer cancel()

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.CIState != "failing" {
		t.Fatalf("expected CIState=failing, got %q", got.CIState)
	}

	if _, ciFailing, _, _ := notify.counts(); ciFailing != 1 {
		t.Fatalf("expected 1 CIFailing call, got %d", ciFailing)
	}

	var sawCIChanged bool
	for {
		select {
		case e := <-sub:
			if e.Type == "pr.ci_changed" {
				sawCIChanged = true
			}
		default:
			goto done
		}
	}
done:
	if !sawCIChanged {
		t.Fatal("expected pr.ci_changed event on bus")
	}
}

func TestTick_Merge_FiresOnce(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	m := newMockGitHubServer()
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	notify := &fakeNotifier{}
	p := New(st, b, ghFactory(srv.URL), testConfig(), notify)

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1 (discovery): %v", err)
	}

	m.mu.Lock()
	m.prState = "closed"
	m.merged = true
	m.mu.Unlock()

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2 (merge): %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRState != "merged" {
		t.Fatalf("expected PRState=merged, got %q", got.PRState)
	}
	if _, _, _, merged := notify.counts(); merged != 1 {
		t.Fatalf("expected 1 Merged call after tick 2, got %d", merged)
	}

	// Third tick: still merged, should not re-fire.
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}
	if _, _, _, merged := notify.counts(); merged != 1 {
		t.Fatalf("expected Merged still called only once after tick 3, got %d", merged)
	}
}

func TestTick_ChangesRequested_FiresOnce(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	m := newMockGitHubServer()
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	notify := &fakeNotifier{}
	p := New(st, b, ghFactory(srv.URL), testConfig(), notify)

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1 (discovery): %v", err)
	}

	m.mu.Lock()
	m.reviews = []reviewStub{{login: "bob", state: "CHANGES_REQUESTED", submittedAt: "2024-01-01T00:00:00Z"}}
	m.mu.Unlock()

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	if _, _, changesRequested, _ := notify.counts(); changesRequested != 1 {
		t.Fatalf("expected 1 ChangesRequested call, got %d", changesRequested)
	}

	// Third tick with the same review decision: no re-fire.
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}
	if _, _, changesRequested, _ := notify.counts(); changesRequested != 1 {
		t.Fatalf("expected ChangesRequested still called only once, got %d", changesRequested)
	}
}

func TestTick_ErrBackoff_AbortsEarly(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")
	addWorker(t, st, "w2", "other-branch")

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	notify := &fakeNotifier{}
	p := New(st, b, ghFactory(srv.URL), testConfig(), notify)

	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick should not return an error on backoff, got: %v", err)
	}

	// Only the first session's request should have gone through before the
	// tick aborted; the second session must be untouched.
	if hits != 1 {
		t.Fatalf("expected exactly 1 HTTP call before abort, got %d", hits)
	}

	got2, err := st.GetSession("w2")
	if err != nil {
		t.Fatalf("GetSession w2: %v", err)
	}
	if got2.PRNumber != 0 {
		t.Fatalf("expected w2 untouched, got %+v", got2)
	}
}

// Ensure errors.Is against github.ErrBackoff is exercised by the client
// (sanity check that the mock 500 truly triggers ErrBackoff).
func TestBackoffSanity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := github.New(srv.URL, "tok")
	_, err := c.FindPRByBranch(context.Background(), "o", "r", "b")
	if !errors.Is(err, github.ErrBackoff) {
		t.Fatalf("expected ErrBackoff, got %v", err)
	}
}
