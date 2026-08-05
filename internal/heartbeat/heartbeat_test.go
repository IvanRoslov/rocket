package heartbeat

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rocket.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testConfig() *config.Config {
	return &config.Config{
		HeartbeatInterval:         5 * time.Minute,
		WorkerStallThreshold:      15 * time.Minute,
		QuestionReminderThreshold: 30 * time.Minute,
		InputStallThreshold:       10 * time.Minute,
		QuestionStaleAfter:        24 * time.Hour,
	}
}

// addCTOAgent registers the persistent agent input-stall escalations go to.
// agent_inbox rows carry a foreign key to agents(id), so the row must exist.
func addCTOAgent(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.AddAgent(store.Agent{ID: escalationAgent, Enabled: true}); err != nil {
		t.Fatalf("AddAgent(%s): %v", escalationAgent, err)
	}
}

// setOrchInputState puts the orchestrator session into an interactive-input
// state: activity (with its timestamp) and, optionally, a pending quiz.
func setOrchInputState(t *testing.T, st *store.Store, id, activityState string, activityTS int64, quiz string) {
	t.Helper()
	if err := st.UpdateSessionActivity(id, activityState, activityTS); err != nil {
		t.Fatalf("UpdateSessionActivity: %v", err)
	}
	if quiz != "" {
		if err := st.SetPendingQuiz(id, quiz); err != nil {
			t.Fatalf("SetPendingQuiz: %v", err)
		}
	}
}

func inboxBodies(t *testing.T, st *store.Store) []string {
	t.Helper()
	msgs, err := st.ListInboxMessages(escalationAgent, "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	var out []string
	for _, m := range msgs {
		out = append(out, m.Body)
	}
	return out
}

func eventTypes(t *testing.T, st *store.Store) []string {
	t.Helper()
	evs, err := st.ListEvents(0, 0, "")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var out []string
	for _, e := range evs {
		out = append(out, e.Type)
	}
	return out
}

func hasEvent(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func TestTick_OrchestratorPendingQuizOverThreshold_EscalatesToCTOInbox(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")
	askedAt := time.Now().Add(-20 * time.Minute).Unix()
	setOrchInputState(t, st, "orch1", "active", time.Now().Unix(),
		`{"questions":[{"question":"Ship or hold?"}],"asked_at":`+strconv.FormatInt(askedAt, 10)+`}`)

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	bodies := inboxBodies(t, st)
	if len(bodies) != 1 {
		t.Fatalf("expected 1 inbox message for %s, got %d", escalationAgent, len(bodies))
	}
	for _, want := range []string{"orch1", "#" + strconv.FormatInt(taskID, 10), "root task", "Ship or hold?", "rocket attach orch1"} {
		if !strings.Contains(bodies[0], want) {
			t.Errorf("expected body to contain %q, got %q", want, bodies[0])
		}
	}
	if !hasEvent(eventTypes(t, st), "orchestrator.input_stalled") {
		t.Errorf("expected an orchestrator.input_stalled event, got %v", eventTypes(t, st))
	}
}

func TestTick_OrchestratorWaitingInputOverThreshold_EscalatesToCTOInbox(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	bodies := inboxBodies(t, st)
	if len(bodies) != 1 {
		t.Fatalf("expected 1 inbox message, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "orch1") || !strings.Contains(bodies[0], "20m") {
		t.Errorf("expected body to name the session and the duration, got %q", bodies[0])
	}

	evs, err := st.ListEvents(0, 0, "")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var found bool
	for _, e := range evs {
		if e.Type != "orchestrator.input_stalled" {
			continue
		}
		found = true
		if e.Data["kind"] != "prompt" {
			t.Errorf("event kind = %v, want prompt", e.Data["kind"])
		}
		if e.SessionID != "orch1" {
			t.Errorf("event session_id = %q, want orch1", e.SessionID)
		}
	}
	if !found {
		t.Error("expected an orchestrator.input_stalled event")
	}
}

func TestTick_OrchestratorWaitingInputBelowThreshold_NoEscalation(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-2*time.Minute).Unix(), "")

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if bodies := inboxBodies(t, st); len(bodies) != 0 {
		t.Fatalf("expected no escalation below threshold, got %v", bodies)
	}
}

// A session waiting on input is by definition not doing anything, but its
// last known activity may still be reported as active by the poller; the
// escalation must not be suppressed by the "don't interrupt an active
// orchestrator" rule that guards the ordinary heartbeat summary, because the
// summary goes to the stalled orchestrator itself and the escalation does not.
func TestTick_InputStalledOrchestratorReportedActive_StillEscalates(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, b, cfg, fixedActivity(activity.Active), func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if bodies := inboxBodies(t, st); len(bodies) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(bodies))
	}
}

func TestTick_InputStallAntiSpam_TwoTicksWithinIntervalEscalateOnce(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	now := time.Now()
	hb.nowFunc = func() time.Time { return now }
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	hb.nowFunc = func() time.Time { return now.Add(1 * time.Minute) }
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	if bodies := inboxBodies(t, st); len(bodies) != 1 {
		t.Fatalf("expected 1 escalation after two ticks within the interval, got %d", len(bodies))
	}
}

func TestTick_InputStallAntiSpam_EscalatesAgainAfterInterval(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()
	addCTOAgent(t, st)

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	now := time.Now()
	hb.nowFunc = func() time.Time { return now }
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	hb.nowFunc = func() time.Time { return now.Add(cfg.HeartbeatInterval + time.Minute) }
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	if bodies := inboxBodies(t, st); len(bodies) != 2 {
		t.Fatalf("expected 2 escalations once the anti-spam window passed, got %d", len(bodies))
	}
}

// The cto agent is a convention, not a guarantee: when it isn't registered
// there is no inbox to write to (agent_inbox has a foreign key to agents),
// and the sweep must degrade to the bus event instead of failing the tick.
func TestTick_InputStalled_NoCTOAgent_PublishesEventWithoutError(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	setOrchInputState(t, st, "orch1", "waiting_input", time.Now().Add(-20*time.Minute).Unix(), "")

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if !hasEvent(eventTypes(t, st), "orchestrator.input_stalled") {
		t.Errorf("expected an orchestrator.input_stalled event, got %v", eventTypes(t, st))
	}
}

func TestInputStall(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	old := now.Add(-20 * time.Minute).Unix()
	fresh := now.Add(-1 * time.Minute).Unix()
	quiz := func(askedAt int64) string {
		return `{"questions":[{"question":"q"}],"asked_at":` + strconv.FormatInt(askedAt, 10) + `}`
	}

	tests := []struct {
		name   string
		sess   store.Session
		wantOK bool
	}{
		{"idle orchestrator", store.Session{Activity: "idle", ActivityTS: old}, false},
		{"active orchestrator", store.Session{Activity: "active", ActivityTS: old}, false},
		{"ready orchestrator", store.Session{Activity: "ready", ActivityTS: old}, false},
		{"fresh waiting_input", store.Session{Activity: "waiting_input", ActivityTS: fresh}, false},
		{"stale waiting_input", store.Session{Activity: "waiting_input", ActivityTS: old}, true},
		{"waiting_input without timestamp", store.Session{Activity: "waiting_input"}, false},
		{"stale quiz while active", store.Session{Activity: "active", ActivityTS: now.Unix(), PendingQuiz: quiz(old)}, true},
		{"fresh quiz", store.Session{Activity: "active", ActivityTS: now.Unix(), PendingQuiz: quiz(fresh)}, false},
		{"quiz without asked_at falls back to activity ts", store.Session{Activity: "active", ActivityTS: old, PendingQuiz: `{"questions":[]}`}, true},
		{"quiz without any timestamp", store.Session{PendingQuiz: `{"questions":[]}`}, false},
		{"unparseable quiz falls back to activity ts", store.Session{Activity: "active", ActivityTS: old, PendingQuiz: `not json`}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, ok := InputStalled(tt.sess, now, 10*time.Minute)
			if ok != tt.wantOK {
				t.Fatalf("InputStalled ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && since <= 10*time.Minute {
				t.Errorf("since = %v, want over the threshold", since)
			}
		})
	}
}

// seedOrchAndTask adds an orchestrator session and its in_progress root task,
// returning the orchestrator id and task id.
func seedOrchAndTask(t *testing.T, st *store.Store, orchID string, taskStatus string) int64 {
	t.Helper()
	if err := st.AddSession(store.Session{
		ID: orchID, Kind: "orchestrator", ProjectID: "proj", RepoID: "repo",
		FeatureSlug: "feat", Agent: "claude-code", Branch: "b", WorktreePath: "/wt/" + orchID,
		TmuxName: "t-" + orchID, State: "running",
	}); err != nil {
		t.Fatalf("AddSession(orch): %v", err)
	}
	taskID, err := st.AddTask(store.Task{
		Title: "root task", ProjectID: "proj", Status: taskStatus,
		FeatureSlug: "feat", SessionID: orchID,
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return taskID
}

func addWorker(t *testing.T, st *store.Store, id, parentID, state, activityState string, activityTS int64) {
	t.Helper()
	if err := st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "proj", RepoID: "repo", FeatureSlug: "feat",
		ParentID: parentID, Agent: "claude-code", Branch: "b-" + id, WorktreePath: "/wt/" + id,
		TmuxName: "t-" + id, State: state, Activity: activityState, ActivityTS: activityTS,
	}); err != nil {
		t.Fatalf("AddSession(worker): %v", err)
	}
}

// fixedActivity returns a getActivity func returning a fixed state for every
// session, always "known".
func fixedActivity(state activity.State) func(string) (activity.State, bool) {
	return func(string) (activity.State, bool) {
		return state, true
	}
}

func unknownActivity(string) (activity.State, bool) {
	return "", false
}

func TestTick_StalledIdleWorker_QueuesSummaryWithWorkerLine(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-20 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "running", "idle", staleTS)

	var woke []string
	hb := New(st, b, cfg, unknownActivity, func(to string) { woke = append(woke, to) })

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Body, "worker1") {
		t.Errorf("expected body to mention worker1, got %q", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, "[rocket heartbeat]") {
		t.Errorf("expected body to have heartbeat prefix, got %q", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, "no PR") {
		t.Errorf("expected body to mention 'no PR', got %q", msgs[0].Body)
	}
	if len(woke) != 1 || woke[0] != "orch1" {
		t.Errorf("expected wake(orch1), got %v", woke)
	}
}

func TestTick_StalledWorkerWithPR_QueuesSummaryWithPRLine(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-20 * time.Minute).Unix()

	// Add worker with PR info
	if err := st.AddSession(store.Session{
		ID: "worker1", Kind: "worker", ProjectID: "proj", RepoID: "repo", FeatureSlug: "feat",
		ParentID: "orch1", Agent: "claude-code", Branch: "b-worker1", WorktreePath: "/wt/worker1",
		TmuxName: "t-worker1", State: "running", Activity: "idle", ActivityTS: staleTS,
		PRNumber: 42, PRState: "open", CIState: "failing",
	}); err != nil {
		t.Fatalf("AddSession(worker): %v", err)
	}

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// Should mention PR #42 and CI state
	if !strings.Contains(msgs[0].Body, "PR #42") {
		t.Errorf("expected body to mention PR #42, got %q", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, "failing") {
		t.Errorf("expected body to mention CI state failing, got %q", msgs[0].Body)
	}
	if strings.Contains(msgs[0].Body, "no PR") {
		t.Errorf("should not mention 'no PR' when PR is set, got %q", msgs[0].Body)
	}
}

func TestTick_FreshIdleWorker_NoHeartbeat(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	freshTS := time.Now().Add(-1 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "running", "idle", freshTS)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages, got %d", len(msgs))
	}
}

func TestTick_ExitedWorker_QueuesSummaryWithExitedLine(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	// "exited" is a store.Session.Activity value (see internal/activity), set
	// by the poller when a worker's tmux pane dies; store.Session.State stays
	// "running" until the reconciler later promotes it to "errored". Seed the
	// same shape production data has: State "running", Activity "exited".
	addWorker(t, st, "worker1", "orch1", "running", "exited", 0)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Body, "worker1: exited") {
		t.Errorf("expected body to mention worker1 exited, got %q", msgs[0].Body)
	}
}

func TestTick_KilledWorker_NoHeartbeat(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	// A killed worker keeps its last Activity ("exited" after the pane died),
	// but it is gone on purpose — it must not be reported as stalled forever.
	addWorker(t, st, "worker1", "orch1", "killed", "exited", 0)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages for a killed worker, got %d: %+v", len(msgs), msgs)
	}
}

func TestTick_DoneWorkerIdleOverThreshold_NoHeartbeat(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-30 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "done", "idle", staleTS)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages for a finished worker, got %d: %+v", len(msgs), msgs)
	}
}

func TestTick_SpawningWorkerIdleOverThreshold_QueuesSummary(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-30 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "spawning", "idle", staleTS)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestTick_OrchestratorActive_Skipped(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-20 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "running", "idle", staleTS)

	hb := New(st, b, cfg, fixedActivity(activity.Active), func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages (orchestrator active), got %d", len(msgs))
	}
}

func TestTick_AntiSpam_TwoTicksWithinIntervalSendOneMessage(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "in_progress")
	staleTS := time.Now().Add(-20 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "running", "idle", staleTS)

	hb := New(st, b, cfg, unknownActivity, func(string) {})
	now := time.Now()
	hb.nowFunc = func() time.Time { return now }

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	// Second tick shortly after, well within HeartbeatInterval.
	hb.nowFunc = func() time.Time { return now.Add(1 * time.Minute) }
	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after two ticks within interval, got %d", len(msgs))
	}
}

func TestTick_TaskInReview_Skipped(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	seedOrchAndTask(t, st, "orch1", "review")
	staleTS := time.Now().Add(-20 * time.Minute).Unix()
	addWorker(t, st, "worker1", "orch1", "running", "idle", staleTS)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages (task in review), got %d", len(msgs))
	}
}

func TestTick_OverdueQuestion_QueuesReminderLine(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")

	qid, err := st.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch1", Body: "Should I use approach A or approach B for the caching layer here?"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	// Seed a human reply older than QuestionReminderThreshold.
	oldTS := time.Now().Add(-45 * time.Minute).Unix()
	if _, err := st.AddQuestionMessage(store.QuestionMessage{
		QuestionID: qid, Author: "", Kind: "reply", Body: "Use approach A.", CreatedAt: oldTS,
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Body, "Q1") || !strings.Contains(msgs[0].Body, "waiting for your reply") {
		t.Errorf("expected body to contain a reminder line, got %q", msgs[0].Body)
	}
}

// TestTick_OverdueQuestion_RuneSafeSnippetTruncation verifies the reminder
// snippet truncates on runes, not bytes: a Russian (multi-byte UTF-8)
// question body over 60 runes must still produce valid UTF-8 output instead
// of slicing mid-rune.
func TestTick_OverdueQuestion_RuneSafeSnippetTruncation(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	taskID := seedOrchAndTask(t, st, "orch1", "in_progress")

	body := "Какой подход лучше выбрать для реализации слоя кэширования в этом сервисе, учитывая нагрузку?"
	if len([]rune(body)) <= 60 {
		t.Fatalf("test body must be over 60 runes, got %d", len([]rune(body)))
	}
	qid, err := st.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch1", Body: body})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	oldTS := time.Now().Add(-45 * time.Minute).Unix()
	if _, err := st.AddQuestionMessage(store.QuestionMessage{
		QuestionID: qid, Author: "", Kind: "reply", Body: "Используй вариант А.", CreatedAt: oldTS,
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !utf8.ValidString(msgs[0].Body) {
		t.Errorf("reminder body is not valid UTF-8: %q", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, "Q1") || !strings.Contains(msgs[0].Body, "waiting for your reply") {
		t.Errorf("expected body to contain a reminder line, got %q", msgs[0].Body)
	}
	wantSnippet := string([]rune(body)[:60])
	if !strings.Contains(msgs[0].Body, wantSnippet) {
		t.Errorf("expected body to contain rune-truncated snippet %q, got %q", wantSnippet, msgs[0].Body)
	}
}

func TestTick_OrchestratorBlocked_Skipped(t *testing.T) {
	st := openTestStore(t)
	b := bus.New(st)
	cfg := testConfig()

	// A "blocked" orchestrator session is terminal-ish for heartbeat
	// purposes: since ListSessions(All:false) only returns spawning|running
	// sessions, a blocked orchestrator (state stored as "blocked") is
	// naturally excluded from the live orchestrator set.
	if err := st.AddSession(store.Session{
		ID: "orch1", Kind: "orchestrator", ProjectID: "proj", RepoID: "repo",
		FeatureSlug: "feat", Agent: "claude-code", Branch: "b", WorktreePath: "/wt/orch1",
		TmuxName: "t-orch1", State: "blocked",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if _, err := st.AddTask(store.Task{
		Title: "root task", ProjectID: "proj", Status: "in_progress",
		FeatureSlug: "feat", SessionID: "orch1",
	}); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	addWorker(t, st, "worker1", "orch1", "exited", "", 0)

	hb := New(st, b, cfg, unknownActivity, func(string) {})

	if err := hb.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	msgs, err := st.ListMessages("orch1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no messages (orchestrator blocked), got %d", len(msgs))
	}
}
