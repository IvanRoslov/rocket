package heartbeat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if len(woke) != 1 || woke[0] != "orch1" {
		t.Errorf("expected wake(orch1), got %v", woke)
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
	addWorker(t, st, "worker1", "orch1", "exited", "", 0)

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
