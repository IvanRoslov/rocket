package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// quizAnswerFakeRuntime is a runtime.Runtime fake that records every
// SendKeys call, for asserting POST /v1/sessions/{id}/quiz/answer actually
// drives the injector.
type quizAnswerFakeRuntime struct {
	mu   sync.Mutex
	sent []string // "<key>" or "-l:<text>"
}

func (f *quizAnswerFakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (f *quizAnswerFakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	return nil
}
func (f *quizAnswerFakeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if literal {
		f.sent = append(f.sent, "-l:"+key)
	} else {
		f.sent = append(f.sent, key)
	}
	return nil
}
func (f *quizAnswerFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (f *quizAnswerFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (f *quizAnswerFakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}
func (f *quizAnswerFakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error {
	return nil
}
func (f *quizAnswerFakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (f *quizAnswerFakeRuntime) AttachCommand(h runtime.Handle) []string {
	return []string{"tmux", "attach", "-t", h.Name}
}
func (f *quizAnswerFakeRuntime) List(ctx context.Context) ([]string, error) { return nil, nil }

func (f *quizAnswerFakeRuntime) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

// quizAnswerTestDeps builds Deps with a real store/bus and a Manager wired
// to a quizAnswerFakeRuntime, for tests exercising
// POST /v1/sessions/{id}/quiz/answer and pending_quiz exposure.
func quizAnswerTestDeps(t *testing.T) (Deps, *quizAnswerFakeRuntime) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir, DefaultAgent: "fake"}
	rt := &quizAnswerFakeRuntime{}
	mgr := session.NewManager(st, b, rt, sessFakeWorkspace{}, cfg)
	mgr.SetQuizTiming(func(time.Duration) {}, 150*time.Millisecond)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Manager = mgr
	return d, rt
}

const quizAPIPendingJSON = `{"questions":[{"question":"Which color?","header":"Color","multiSelect":false,"options":[{"label":"Red","description":"warm"},{"label":"Green"},{"label":"Blue"}]}],"asked_at":42}`

// quizAPITenOptionPendingJSON has 10 options, enough for an in-range
// option_index (9) that still exceeds the single-digit limit remote
// answering supports (see TestPostQuizAnswer_InvalidAnswerIs400).
const quizAPITenOptionPendingJSON = `{"questions":[{"question":"Pick a number","header":"Number","multiSelect":false,"options":[{"label":"0"},{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"},{"label":"5"},{"label":"6"},{"label":"7"},{"label":"8"},{"label":"9"}]}],"asked_at":42}`

func seedQuizSession(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "proj1", Name: "proj1", MainRepo: "repo1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "proj1", RepoID: "repo1",
		FeatureSlug: "feat1", Branch: "feature/feat1/task1",
		WorktreePath: "/tmp/wt/" + id, TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

// --- pending_quiz exposure ---------------------------------------------

func TestGetSession_PendingQuizOmittedWhenNone(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")

	resp, err := http.Get(srv.URL + "/v1/sessions/sess1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["pending_quiz"]; ok {
		t.Errorf("pending_quiz present = %v, want omitted", body["pending_quiz"])
	}
}

func TestGetSession_PendingQuizShape(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	if err := d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/sessions/sess1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		PendingQuiz *quizResponse `json:"pending_quiz"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.PendingQuiz == nil {
		t.Fatal("pending_quiz = nil, want present")
	}
	if body.PendingQuiz.AskedAt != 42 {
		t.Errorf("asked_at = %d, want 42", body.PendingQuiz.AskedAt)
	}
	if len(body.PendingQuiz.Questions) != 1 {
		t.Fatalf("len(questions) = %d, want 1", len(body.PendingQuiz.Questions))
	}
	q := body.PendingQuiz.Questions[0]
	if q.Question != "Which color?" || q.Header != "Color" || q.MultiSelect {
		t.Errorf("question = %+v, want {question:Which color? header:Color multi_select:false}", q)
	}
	if len(q.Options) != 3 || q.Options[0].Label != "Red" || q.Options[0].Description != "warm" {
		t.Errorf("options = %+v", q.Options)
	}

	// multi_select must be the wire field name (snake_case), not the
	// storage-shape multiSelect.
	raw, err := http.Get(srv.URL + "/v1/sessions/sess1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer raw.Body.Close()
	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(raw.Body).Decode(&rawBody); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	pqStr := string(rawBody["pending_quiz"])
	if !jsonContains(pqStr, `"multi_select"`) {
		t.Errorf("pending_quiz body = %s, want to contain \"multi_select\"", pqStr)
	}
	if jsonContains(pqStr, `"multiSelect"`) {
		t.Errorf("pending_quiz body = %s, want NOT to contain \"multiSelect\"", pqStr)
	}
}

func jsonContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestChatSession_PendingQuizExposed(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	if err := d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/sessions/sess1/chat")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Session struct {
			PendingQuiz *quizResponse `json:"pending_quiz"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session.PendingQuiz == nil {
		t.Fatal("session.pending_quiz = nil, want present")
	}
}

// --- POST /v1/sessions/{id}/quiz/answer ---------------------------------

func TestPostQuizAnswer_HappyPathSingleSelect(t *testing.T) {
	d, rt := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	if err := d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	// Drain the background unconfirmed-watch goroutine's event before this
	// test (and its store) tears down — quizAnswerTestDeps configures a
	// short (150ms) unconfirmed timeout, so without waiting for it here
	// the watcher would still be running past t.Cleanup's store.Close(),
	// logging a spurious "database is closed" error when it publishes.
	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{1}}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "answering" {
		t.Errorf("status = %q, want answering", body["status"])
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(rt.snapshot()) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for keystroke injection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sent := rt.snapshot()
	want := []string{"2", "Enter"} // option index 1 -> digit "2", then final submit Enter
	if len(sent) != len(want) {
		t.Fatalf("sent = %v, want %v", sent, want)
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Errorf("sent[%d] = %q, want %q", i, sent[i], want[i])
		}
	}

	waitDeadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.quiz_answer_unconfirmed" {
				return
			}
		case <-waitDeadline:
			t.Fatal("timed out waiting for the background unconfirmed watch to finish")
		}
	}
}

func TestPostQuizAnswer_UnknownSessionIs404(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/sessions/does-not-exist/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := decodeErr(t, resp)
	if body.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", body.Error.Code)
	}
}

func TestPostQuizAnswer_NoPendingQuizIs409(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	body := decodeErr(t, resp)
	if body.Error.Code != "no_pending_quiz" {
		t.Errorf("code = %q, want no_pending_quiz", body.Error.Code)
	}
}

func TestPostQuizAnswer_InvalidAnswerIs400(t *testing.T) {
	tests := []struct {
		name        string
		pendingJSON string
		answers     []map[string]any
	}{
		{"index out of range", quizAPIPendingJSON, []map[string]any{{"question_index": 7, "option_indices": []int{0}}}},
		{"single-select not exactly one", quizAPIPendingJSON, []map[string]any{{"question_index": 0, "option_indices": []int{0, 1}}}},
		{"both option_indices and text", quizAPIPendingJSON, []map[string]any{{"question_index": 0, "option_indices": []int{0}, "text": "x"}}},
		{"empty text", quizAPIPendingJSON, []map[string]any{{"question_index": 0, "text": ""}}},
		{"not all questions answered", quizAPIPendingJSON, []map[string]any{}},
		// LOW finding #4: option_index >= 9 is rejected even when in
		// range for the question, since remote answering can only type
		// single-digit option numbers reliably. quizAPITenOptionPendingJSON
		// has 10 options so index 9 is in-range but still over the limit.
		{"option_index 9 exceeds single-digit limit", quizAPITenOptionPendingJSON, []map[string]any{{"question_index": 0, "option_indices": []int{9}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := quizAnswerTestDeps(t)
			srv := newTestServer(t, d)
			seedQuizSession(t, d.Store, "sess1")
			if err := d.Store.SetPendingQuiz("sess1", tt.pendingJSON); err != nil {
				t.Fatalf("SetPendingQuiz: %v", err)
			}

			resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{"answers": tt.answers})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			body := decodeErr(t, resp)
			if body.Error.Code != "quiz_answer_invalid" {
				t.Errorf("code = %q, want quiz_answer_invalid", body.Error.Code)
			}
		})
	}
}

// TestPostQuizAnswer_InFlightIs409 verifies HIGH finding #1 end-to-end
// through the HTTP layer: a second POST while the first answer's
// keystroke injection is still running is rejected 409
// quiz_answer_in_flight, and once the first injection resolves, a new
// answer is accepted again.
func TestPostQuizAnswer_InFlightIs409(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	// A real, longer-than-instant sleep between keystrokes so the first
	// injection is still in flight when the second POST lands right after.
	d.Manager.SetQuizTiming(func(time.Duration) { time.Sleep(30 * time.Millisecond) }, 2*time.Second)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	if err := d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	resp1 := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
	})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202", resp1.StatusCode)
	}

	resp2 := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{1}}},
	})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second status = %d, want 409", resp2.StatusCode)
	}
	body := decodeErr(t, resp2)
	if body.Error.Code != "quiz_answer_in_flight" {
		t.Errorf("code = %q, want quiz_answer_in_flight", body.Error.Code)
	}

	// Resolve the first injection so its in-flight flag clears.
	d.Bus.Publish("session.quiz_resolved", "sess1", map[string]any{})

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp3 := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
			"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
		})
		status := resp3.StatusCode
		resp3.Body.Close()
		if status == http.StatusAccepted {
			d.Bus.Publish("session.quiz_resolved", "sess1", map[string]any{})
			return
		}
		if status != http.StatusConflict {
			t.Fatalf("unexpected status while polling for in-flight clear: %d", status)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for in-flight flag to clear after resolved")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPostQuizAnswer_UnconfirmedTimerFires exercises the end-to-end
// unconfirmed path through the HTTP layer: a valid answer with no matching
// session.quiz_resolved event published afterward must surface
// session.quiz_answer_unconfirmed on the bus within the (short,
// test-only) timeout quizAnswerTestDeps configures.
func TestPostQuizAnswer_UnconfirmedTimerFires(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	if err := d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.quiz_answer_unconfirmed" && ev.SessionID == "sess1" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for session.quiz_answer_unconfirmed")
		}
	}
}
