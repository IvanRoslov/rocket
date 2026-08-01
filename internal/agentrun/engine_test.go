package agentrun

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeSpawner records what the engine asked for and simulates a live
// instance, so engine tests never touch tmux or git.
type fakeSpawner struct {
	st *store.Store

	// mu guards everything below: Notify processes roles on a timer
	// goroutine, so the test goroutine and the engine touch these
	// concurrently.
	mu sync.Mutex

	spawns    []string // briefings, in order
	spawnRole []string // role ids, in order
	killed    []string
	spawnErr  error

	// live, when non-empty, is the id of the role instance the engine
	// should consider alive.
	live map[string]store.Session
}

func newFakeSpawner(st *store.Store) *fakeSpawner {
	return &fakeSpawner{st: st, live: map[string]store.Session{}}
}

func (f *fakeSpawner) SpawnRole(ctx context.Context, role store.Agent, briefing string) (store.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spawnErr != nil {
		return store.Session{}, f.spawnErr
	}
	f.spawnRole = append(f.spawnRole, role.ID)
	f.spawns = append(f.spawns, briefing)
	sess := store.Session{
		ID: role.ID + "-run-" + itoa(int64(len(f.spawns))), Kind: "agent",
		ProjectID: role.ProjectID, State: "running",
	}
	f.live[role.ID] = sess
	return sess, nil
}

func (f *fakeSpawner) LiveRoleInstance(roleID string) (store.Session, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.live[roleID]
	return s, ok, nil
}

func (f *fakeSpawner) Kill(ctx context.Context, id string, cleanup bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, id)
	for role, s := range f.live {
		if s.ID == id {
			delete(f.live, role)
		}
	}
	return f.st.UpdateSessionState(id, "killed")
}

// briefings returns a snapshot of the briefings the engine spawned with.
func (f *fakeSpawner) briefings() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spawns...)
}

// killedIDs returns a snapshot of the sessions the engine killed.
func (f *fakeSpawner) killedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killed...)
}

// setLive marks a session as the role's live instance.
func (f *fakeSpawner) setLive(roleID string, sess store.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live[roleID] = sess
}

type engineFixture struct {
	engine *Engine
	st     *store.Store
	sp     *fakeSpawner
	home   string
	role   store.Agent
	now    time.Time
}

func newEngineFixture(t *testing.T) *engineFixture {
	t.Helper()
	st, home := openTestStore(t)

	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "platform", Name: "Platform", MainRepo: "repo1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	promptPath, err := roles.Ensure(home, "sre", "POLICY", true)
	if err != nil {
		t.Fatalf("roles.Ensure: %v", err)
	}
	role := store.Agent{ID: "sre", ProjectID: "platform", PromptPath: promptPath, Enabled: true}
	if err := st.AddAgent(role); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	cfg := &config.Config{
		Home:              home,
		AgentIdleTimeout:  15 * time.Minute,
		AgentWakeDebounce: 10 * time.Millisecond,
	}

	f := &engineFixture{
		st: st, home: home, role: role, sp: newFakeSpawner(st),
		now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}
	f.engine = New(st, bus.New(st), cfg, f.sp, func(string) {})
	f.engine.now = func() time.Time { return f.now }
	return f
}

func (f *engineFixture) enqueue(t *testing.T, kind, payload string) int64 {
	t.Helper()
	id, err := f.st.EnqueueInboxEvent(store.AgentInboxEvent{RoleID: "sre", Kind: kind, Payload: payload})
	if err != nil {
		t.Fatalf("EnqueueInboxEvent: %v", err)
	}
	return id
}

func (f *engineFixture) inboxStatuses(t *testing.T) map[int64]string {
	t.Helper()
	events, err := f.st.ListInboxEvents("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxEvents: %v", err)
	}
	out := map[int64]string{}
	for _, e := range events {
		out[e.ID] = e.Status
	}
	return out
}

func TestProcessRoleSpawnsOneInstanceForABatchOfEvents(t *testing.T) {
	f := newEngineFixture(t)
	a := f.enqueue(t, "message", `{"text":"blocked by X","from":"feat-orch"}`)
	b := f.enqueue(t, "message", `{"text":"and also Y"}`)

	f.engine.Tick(context.Background())

	if len(f.sp.briefings()) != 1 {
		t.Fatalf("SpawnRole calls = %d, want 1", len(f.sp.briefings()))
	}
	briefing := f.sp.briefings()[0]
	for _, want := range []string{"blocked by X", "and also Y", "rocket agent done"} {
		if !strings.Contains(briefing, want) {
			t.Errorf("briefing missing %q:\n%s", want, briefing)
		}
	}

	statuses := f.inboxStatuses(t)
	if statuses[a] != store.InboxStatusDelivered || statuses[b] != store.InboxStatusDelivered {
		t.Errorf("events not marked delivered: %v", statuses)
	}
}

func TestProcessRoleInjectsIntoLiveInstance(t *testing.T) {
	f := newEngineFixture(t)
	live := store.Session{
		ID: "sre-run-1", Kind: "agent", ProjectID: "platform", RepoID: "repo1",
		FeatureSlug: "sre", Agent: "fake", Branch: "agent/sre", TmuxName: "sre-run-1",
		State: "running",
	}
	if err := f.st.AddSession(live); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	f.sp.setLive("sre", live)

	var woken []string
	f.engine.queueWake = func(to string) { woken = append(woken, to) }

	id := f.enqueue(t, "message", `{"text":"one more thing","from":"feat-orch"}`)
	f.engine.Tick(context.Background())

	if len(f.sp.briefings()) != 0 {
		t.Fatalf("spawned %d instances while one was live", len(f.sp.briefings()))
	}

	msgs, err := f.st.ListMessages("sre-run-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages queued = %d, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Body, "one more thing") {
		t.Errorf("message body = %q", msgs[0].Body)
	}
	if msgs[0].ToSession != "sre-run-1" {
		t.Errorf("message to = %q", msgs[0].ToSession)
	}
	if len(woken) != 1 || woken[0] != "sre-run-1" {
		t.Errorf("queue wakes = %v, want [sre-run-1]", woken)
	}
	if f.inboxStatuses(t)[id] != store.InboxStatusDelivered {
		t.Errorf("event not marked delivered")
	}
}

func TestProcessRoleSkipsDisabledRole(t *testing.T) {
	f := newEngineFixture(t)
	f.role.Enabled = false
	if err := f.st.UpdateAgent(f.role); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	id := f.enqueue(t, "message", `{"text":"hi"}`)

	f.engine.Tick(context.Background())

	if len(f.sp.briefings()) != 0 {
		t.Fatalf("spawned an instance of a disabled role")
	}
	if f.inboxStatuses(t)[id] != store.InboxStatusQueued {
		t.Errorf("event of a disabled role must stay queued")
	}
}

func TestNotifyDebouncesIntoASingleSpawn(t *testing.T) {
	f := newEngineFixture(t)
	f.enqueue(t, "message", `{"text":"first"}`)
	f.engine.Notify("sre")
	f.enqueue(t, "message", `{"text":"second"}`)
	f.engine.Notify("sre")

	waitFor(t, time.Second, func() bool { return len(f.sp.briefings()) == 1 })

	if len(f.sp.briefings()) != 1 {
		t.Fatalf("SpawnRole calls = %d, want exactly 1", len(f.sp.briefings()))
	}
	if !strings.Contains(f.sp.briefings()[0], "second") {
		t.Errorf("the debounced batch lost the second event:\n%s", f.sp.briefings()[0])
	}
}

func TestDoneClosesEventsAndKillsInstance(t *testing.T) {
	f := newEngineFixture(t)
	id := f.enqueue(t, "message", `{"text":"hi"}`)
	f.engine.Tick(context.Background())

	live := store.Session{
		ID: "sre-run-1", Kind: "agent", ProjectID: "platform", RepoID: "repo1",
		FeatureSlug: "sre", Agent: "fake", Branch: "agent/sre", TmuxName: "sre-run-1",
		State: "running",
	}
	if err := f.st.AddSession(live); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	if err := f.engine.Done(context.Background(), "sre-run-1"); err != nil {
		t.Fatalf("Done: %v", err)
	}

	if len(f.sp.killedIDs()) != 1 || f.sp.killedIDs()[0] != "sre-run-1" {
		t.Fatalf("killed = %v, want [sre-run-1]", f.sp.killedIDs())
	}
	if got := f.inboxStatuses(t)[id]; got != store.InboxStatusDone {
		t.Errorf("event status = %q, want done", got)
	}
}

func TestDoneRejectsNonInstanceSession(t *testing.T) {
	f := newEngineFixture(t)
	if err := f.engine.Done(context.Background(), "myfeat-mytask"); err == nil {
		t.Fatal("Done on a non-instance session: want error")
	}
}

func TestTickWakesOnExpiredSnooze(t *testing.T) {
	f := newEngineFixture(t)
	it, err := f.st.UpsertAgentItem(store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#1",
		State: "deferred", Note: "ждём миграцию", SnoozeUntil: f.now.Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("UpsertAgentItem: %v", err)
	}

	f.engine.Tick(context.Background())

	events, err := f.st.ListInboxEvents("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "snooze_expired" {
		t.Fatalf("events = %+v, want one snooze_expired", events)
	}
	if !strings.Contains(events[0].Payload, "acme/platform#1") {
		t.Errorf("snooze payload = %q", events[0].Payload)
	}
	if len(f.sp.briefings()) != 1 {
		t.Errorf("expired snooze did not wake the role: %d spawns", len(f.sp.briefings()))
	}

	reloaded, err := f.st.GetAgentItem("sre", "issue", "acme/platform#1")
	if err != nil {
		t.Fatalf("GetAgentItem: %v", err)
	}
	if reloaded.SnoozeUntil != 0 {
		t.Errorf("snooze deadline not cleared, item %d still has %d", it.ID, reloaded.SnoozeUntil)
	}

	// A second tick must not fire the same snooze again.
	f.engine.Tick(context.Background())
	events, _ = f.st.ListInboxEvents("sre", "", 0)
	if len(events) != 1 {
		t.Errorf("snooze fired more than once: %+v", events)
	}
}

func TestTickFiresRoleCron(t *testing.T) {
	f := newEngineFixture(t)
	f.role.Cron = "0 * * * *"
	if err := f.st.UpdateAgent(f.role); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	// First tick only arms the schedule (next fire at 13:00).
	f.engine.Tick(context.Background())
	if events, _ := f.st.ListInboxEvents("sre", "", 0); len(events) != 0 {
		t.Fatalf("cron fired on the arming tick: %+v", events)
	}

	f.now = f.now.Add(61 * time.Minute)
	f.engine.Tick(context.Background())

	events, _ := f.st.ListInboxEvents("sre", "", 0)
	if len(events) != 1 || events[0].Kind != "cron" {
		t.Fatalf("events = %+v, want one cron", events)
	}
	if len(f.sp.briefings()) != 1 {
		t.Errorf("cron did not wake the role: %d spawns", len(f.sp.briefings()))
	}
}

func TestTickIgnoresInvalidCron(t *testing.T) {
	f := newEngineFixture(t)
	f.role.Cron = "not a cron"
	if err := f.st.UpdateAgent(f.role); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	f.now = f.now.Add(2 * time.Hour)
	f.engine.Tick(context.Background())

	if events, _ := f.st.ListInboxEvents("sre", "", 0); len(events) != 0 {
		t.Fatalf("invalid cron produced events: %+v", events)
	}
}

func TestTickKillsIdleInstance(t *testing.T) {
	f := newEngineFixture(t)
	id := f.enqueue(t, "message", `{"text":"hi"}`)
	f.engine.Tick(context.Background())

	live := store.Session{
		ID: "sre-run-1", Kind: "agent", ProjectID: "platform", RepoID: "repo1",
		FeatureSlug: "sre", Agent: "fake", Branch: "agent/sre", TmuxName: "sre-run-1",
		State: "running",
	}
	if err := f.st.AddSession(live); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := f.st.UpdateSessionActivity("sre-run-1", "idle", f.now.Add(-20*time.Minute).Unix()); err != nil {
		t.Fatalf("UpdateSessionActivity: %v", err)
	}

	f.engine.Tick(context.Background())

	if len(f.sp.killedIDs()) != 1 || f.sp.killedIDs()[0] != "sre-run-1" {
		t.Fatalf("killed = %v, want [sre-run-1]", f.sp.killedIDs())
	}
	if got := f.inboxStatuses(t)[id]; got != store.InboxStatusDone {
		t.Errorf("event status after timeout = %q, want done", got)
	}
}

func TestTickKeepsActiveInstanceAlive(t *testing.T) {
	f := newEngineFixture(t)
	live := store.Session{
		ID: "sre-run-1", Kind: "agent", ProjectID: "platform", RepoID: "repo1",
		FeatureSlug: "sre", Agent: "fake", Branch: "agent/sre", TmuxName: "sre-run-1",
		State: "running",
	}
	if err := f.st.AddSession(live); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := f.st.UpdateSessionActivity("sre-run-1", "active", f.now.Add(-20*time.Minute).Unix()); err != nil {
		t.Fatalf("UpdateSessionActivity: %v", err)
	}

	f.engine.Tick(context.Background())

	if len(f.sp.killedIDs()) != 0 {
		t.Fatalf("killed an actively working instance: %v", f.sp.killedIDs())
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestQuestionEventsKeepTheThreadPrefix(t *testing.T) {
	f := newEngineFixture(t)
	live := store.Session{
		ID: "sre-run-1", Kind: "agent", ProjectID: "platform", RepoID: "repo1",
		FeatureSlug: "sre", Agent: "fake", Branch: "agent/sre", TmuxName: "sre-run-1",
		State: "running",
	}
	if err := f.st.AddSession(live); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	f.sp.setLive("sre", live)

	f.enqueue(t, "question", `{"question_id":7,"role_id":"sre","ordinal":2,"entry":"reply","text":"уточняю: только прод"}`)
	f.engine.Tick(context.Background())

	msgs, err := f.st.ListMessages("sre-run-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %+v, want 1", msgs)
	}
	want := "[role sre Q2 reply] уточняю: только прод"
	if msgs[0].Body != want {
		t.Fatalf("body = %q, want %q", msgs[0].Body, want)
	}
}

func TestQuestionEventsInBriefingKeepTheThreadPrefix(t *testing.T) {
	f := newEngineFixture(t)
	f.enqueue(t, "question", `{"question_id":7,"role_id":"sre","ordinal":1,"entry":"question","text":"почему упал деплой?"}`)
	f.engine.Tick(context.Background())

	if len(f.sp.briefings()) != 1 {
		t.Fatalf("spawns = %d, want 1", len(f.sp.briefings()))
	}
	if !strings.Contains(f.sp.briefings()[0], "Q1 question") ||
		!strings.Contains(f.sp.briefings()[0], "почему упал деплой?") {
		t.Fatalf("briefing does not carry the thread entry:\n%s", f.sp.briefings()[0])
	}
}
