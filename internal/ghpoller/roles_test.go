package ghpoller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/store"
)

// roleGitHub is a fake GitHub serving the two endpoints the role poller uses:
// the repository issue list and the repository-wide issue comment list.
type roleGitHub struct {
	mu       sync.Mutex
	issues   []map[string]any
	comments []map[string]any

	issueCalls   int
	commentCalls int
	lastSince    string
}

func (g *roleGitHub) setIssues(issues ...map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.issues = issues
}

func (g *roleGitHub) setComments(comments ...map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.comments = comments
}

func (g *roleGitHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.lastSince = r.URL.Query().Get("since")

		switch {
		case strings.HasSuffix(r.URL.Path, "/issues/comments"):
			g.commentCalls++
			_ = json.NewEncoder(w).Encode(g.comments)
		case strings.HasSuffix(r.URL.Path, "/issues"):
			g.issueCalls++
			_ = json.NewEncoder(w).Encode(g.issues)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})
}

func issueJSON(number int, title, body, author string, createdAt string, labels ...string) map[string]any {
	labelObjs := make([]map[string]any, 0, len(labels))
	for _, l := range labels {
		labelObjs = append(labelObjs, map[string]any{"name": l})
	}
	return map[string]any{
		"number":     number,
		"title":      title,
		"body":       body,
		"state":      "open",
		"html_url":   "https://github.com/o/r/issues/" + itoa(number),
		"created_at": createdAt,
		"updated_at": createdAt,
		"user":       map[string]any{"login": author},
		"labels":     labelObjs,
	}
}

func commentJSON(id int64, issueNumber int, body, author, createdAt string) map[string]any {
	return map[string]any{
		"id":         id,
		"body":       body,
		"created_at": createdAt,
		"html_url":   "https://github.com/o/r/issues/" + itoa(issueNumber) + "#issuecomment-" + itoa(int(id)),
		"issue_url":  "https://api.github.com/repos/o/r/issues/" + itoa(issueNumber),
		"user":       map[string]any{"login": author},
	}
}

// setupRoleEnv creates a store with a project and one enabled role subscribed
// to o/r with the given subscription settings.
func setupRoleEnv(t *testing.T, sub store.AgentSubscription) (*store.Store, *bus.Bus) {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.AddRepo(store.Repo{ID: "platform-repo", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "platform", Name: "Platform", MainRepo: "platform-repo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := st.AddAgent(store.Agent{
		ID: "sre", ProjectID: "platform", PromptPath: "role.md",
		Subscriptions: []store.AgentSubscription{sub}, Enabled: true,
	}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	return st, bus.New(st)
}

// queuedKinds returns the kinds of the role's queued inbox events, oldest first.
func queuedKinds(t *testing.T, st *store.Store) []string {
	t.Helper()
	events, err := st.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	kinds := make([]string, 0, len(events))
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func TestRolePoll_FirstTickSeedsWithoutEnqueue(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	gh := &roleGitHub{}
	gh.setIssues(issueJSON(1, "old backlog", "body", "alice", "2026-01-01T00:00:00Z"))
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if kinds := queuedKinds(t, st); len(kinds) != 0 {
		t.Fatalf("seed tick must not enqueue, got %v", kinds)
	}
	since, err := st.AgentGHWatermark("sre", "o/r")
	if err != nil {
		t.Fatalf("AgentGHWatermark: %v", err)
	}
	if since == 0 {
		t.Fatal("expected the seed tick to record a watermark")
	}
}

// rewindWatermark moves the subscription watermark far into the past so the
// following tick treats the fixture issues/comments as newly created.
func rewindWatermark(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.SetAgentGHWatermark("sre", "o/r", 1000); err != nil {
		t.Fatalf("SetAgentGHWatermark: %v", err)
	}
}

func TestRolePoll_NewIssueEnqueuedOnceAndSurvivesRestart(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	gh.setIssues(issueJSON(42, "disk full", "no space left", "alice", "2026-08-02T10:00:00Z", "ops"))
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, err := st.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "issue_opened" {
		t.Fatalf("expected one issue_opened event, got %+v", events)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["repo"] != "o/r" || payload["number"] != float64(42) ||
		payload["title"] != "disk full" || payload["author"] != "alice" ||
		payload["body"] != "no space left" ||
		payload["html_url"] != "https://github.com/o/r/issues/42" {
		t.Fatalf("unexpected payload: %v", payload)
	}
	labels, _ := payload["labels"].([]any)
	if len(labels) != 1 || labels[0] != "ops" {
		t.Fatalf("unexpected labels: %v", payload["labels"])
	}

	// Same data on the next tick: no duplicate.
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick (repeat): %v", err)
	}
	if kinds := queuedKinds(t, st); len(kinds) != 1 {
		t.Fatalf("expected no duplicate, got %v", kinds)
	}

	// A fresh Poller over the same store stands in for a daemon restart: the
	// dedup state is durable, not in-memory.
	rewindWatermark(t, st)
	restarted := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := restarted.Tick(context.Background()); err != nil {
		t.Fatalf("Tick (restart): %v", err)
	}
	if kinds := queuedKinds(t, st); len(kinds) != 1 {
		t.Fatalf("restart re-enqueued events: %v", kinds)
	}
}

func TestRolePoll_LabelFilter(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r", Labels: []string{"ops", "incident"}})
	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	gh.setIssues(
		issueJSON(1, "docs typo", "body", "alice", "2026-08-02T10:00:00Z", "docs"),
		issueJSON(2, "pager storm", "body", "alice", "2026-08-02T10:01:00Z", "docs", "incident"),
	)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, _ := st.QueuedInboxEvents("sre")
	if len(events) != 1 {
		t.Fatalf("expected only the label match, got %d events", len(events))
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(events[0].Payload), &payload)
	if payload["number"] != float64(2) {
		t.Fatalf("wrong issue matched: %v", payload)
	}
}

func TestRolePoll_MentionOnlyFilter(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r", MentionOnly: true})
	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	gh.setIssues(
		issueJSON(1, "quiet issue", "nothing for anyone", "alice", "2026-08-02T10:00:00Z"),
		issueJSON(2, "ping", "cc @sre please look", "alice", "2026-08-02T10:01:00Z"),
		issueJSON(3, "near miss", "ask @sreteam instead", "alice", "2026-08-02T10:02:00Z"),
	)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, _ := st.QueuedInboxEvents("sre")
	if len(events) != 1 {
		t.Fatalf("expected only the mention, got %d events", len(events))
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(events[0].Payload), &payload)
	if payload["number"] != float64(2) {
		t.Fatalf("wrong issue matched: %v", payload)
	}
}

func TestRolePoll_CommentsOnDossierIssuesAndMentions(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	if _, err := st.UpsertAgentItem(store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "o/r#5", State: "taken",
	}); err != nil {
		t.Fatalf("UpsertAgentItem: %v", err)
	}

	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	gh.setComments(
		// On a dossier issue: the spec's reopen scenario, no mention needed.
		commentJSON(900, 5, "does not work, please fix", "owner", "2026-08-02T11:00:00Z"),
		// On an unrelated issue without a mention: ignored.
		commentJSON(901, 77, "unrelated chatter", "bob", "2026-08-02T11:01:00Z"),
		// On an unrelated issue but mentioning the role: kept.
		commentJSON(902, 78, "@sre can you take this?", "bob", "2026-08-02T11:02:00Z"),
	)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, _ := st.QueuedInboxEvents("sre")
	if len(events) != 2 {
		t.Fatalf("expected 2 comment events, got %d", len(events))
	}
	var first map[string]any
	_ = json.Unmarshal([]byte(events[0].Payload), &first)
	if events[0].Kind != "issue_comment" || first["comment_id"] != float64(900) ||
		first["number"] != float64(5) || first["author"] != "owner" ||
		first["body"] != "does not work, please fix" {
		t.Fatalf("unexpected comment payload: %v", first)
	}
}

func TestRolePoll_SkipsRoleAuthoredMarkerBodies(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	if _, err := st.UpsertAgentItem(store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "o/r#5", State: "taken",
	}); err != nil {
		t.Fatalf("UpsertAgentItem: %v", err)
	}

	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	gh.setComments(
		// The role's own write: skipped even though it mentions @sre.
		commentJSON(900, 5, "took it, @sre on it\n\n<!-- rocket-agent:sre -->", "owner", "2026-08-02T11:00:00Z"),
		// Another role's write: skipped too, so roles cannot loop on each other.
		commentJSON(901, 5, "handing over\n<!-- rocket-agent:triage -->", "owner", "2026-08-02T11:01:00Z"),
		// A plain human comment on the same issue: kept.
		commentJSON(902, 5, "thanks, still broken", "owner", "2026-08-02T11:02:00Z"),
	)
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, _ := st.QueuedInboxEvents("sre")
	if len(events) != 1 {
		t.Fatalf("expected only the human comment, got %d events", len(events))
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(events[0].Payload), &payload)
	if payload["comment_id"] != float64(902) {
		t.Fatalf("wrong comment kept: %v", payload)
	}
}

func TestRolePoll_DisabledRoleIsNotPolled(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	role, err := st.GetAgent("sre")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	role.Enabled = false
	if err := st.UpdateAgent(role); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	gh := &roleGitHub{}
	gh.setIssues(issueJSON(1, "anything", "body", "alice", "2026-08-02T10:00:00Z"))
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	gh.mu.Lock()
	calls := gh.issueCalls + gh.commentCalls
	gh.mu.Unlock()
	if calls != 0 {
		t.Fatalf("disabled role must not be polled, got %d GitHub calls", calls)
	}
	if kinds := queuedKinds(t, st); len(kinds) != 0 {
		t.Fatalf("disabled role must not receive events, got %v", kinds)
	}
}

func TestRolePoll_TruncatesLongBodies(t *testing.T) {
	st, b := setupRoleEnv(t, store.AgentSubscription{Repo: "o/r"})
	gh := &roleGitHub{}
	srv := httptest.NewServer(gh.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("seed Tick: %v", err)
	}
	rewindWatermark(t, st)

	long := strings.Repeat("x", maxPayloadBody+500)
	gh.setIssues(issueJSON(1, "huge", long, "alice", "2026-08-02T10:00:00Z"))
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, _ := st.QueuedInboxEvents("sre")
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(events[0].Payload), &payload)
	body, _ := payload["body"].(string)
	if len(body) > maxPayloadBody+len(truncationMarker) {
		t.Fatalf("body not truncated: %d bytes", len(body))
	}
	if !strings.HasSuffix(body, truncationMarker) {
		t.Fatalf("expected a truncation marker, got tail %q", body[len(body)-10:])
	}
}
