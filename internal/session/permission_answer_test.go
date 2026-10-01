package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

func paneFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// seedPermissionQuiz puts sess1 on the dialog of fixture: journal row,
// pending permission quiz, and a pane that shows the dialog. Returns the
// manager, store, runtime and the journal row id.
func seedPermissionQuiz(t *testing.T, fixture string) (*Manager, *store.Store, *fakeRuntime, int64) {
	t.Helper()
	m, st, _, rt, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	pane := paneFixture(t, fixture)
	p, ok := runtime.ParsePermissionPrompt(pane)
	if !ok {
		t.Fatalf("fixture %s not parsed", fixture)
	}
	id, _, err := st.OpenPermissionPrompt("sess1", p.Title, p.Context, "[]", 1)
	if err != nil {
		t.Fatalf("OpenPermissionPrompt: %v", err)
	}
	b, _ := json.Marshal(NewPermissionQuiz(p, id, 1))
	if err := st.SetPendingQuiz("sess1", string(b)); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
	rt.captureOut = pane
	return m, st, rt, id
}

// sentKeysEventually waits for the async injector to send want keys (or
// the deadline), then returns everything sent.
func sentKeysEventually(rt *fakeRuntime, want int) []sentKey {
	deadline := time.Now().Add(time.Second)
	for {
		rt.mu.Lock()
		sent := append([]sentKey(nil), rt.sentKeys...)
		rt.mu.Unlock()
		if len(sent) >= want || time.Now().After(deadline) {
			time.Sleep(20 * time.Millisecond) // catch any extra key
			rt.mu.Lock()
			sent = append([]sentKey(nil), rt.sentKeys...)
			rt.mu.Unlock()
			return sent
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func answerLabelOf(t *testing.T, st *store.Store, id int64) (label, via string) {
	t.Helper()
	if err := st.ResolvePermissionPrompt(id, 99); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListResolvedPermissionPrompts("sess1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id {
			return r.AnswerLabel, r.AnsweredVia
		}
	}
	t.Fatalf("row %d not found", id)
	return "", ""
}

func TestAnswerPermission_OneDigitNoEnter(t *testing.T) {
	m, st, rt, id := seedPermissionQuiz(t, "permission-edit-settings.pane")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sent := sentKeysEventually(rt, 1)
	if len(sent) != 1 || sent[0] != (sentKey{handle: "sess1", key: "2"}) {
		t.Fatalf("sent = %+v, want exactly digit 2 and no Enter", sent)
	}
	label, via := answerLabelOf(t, st, id)
	if label != "Yes, and allow Claude to edit files in this project's .claude folder for this session" || via != "chat" {
		t.Errorf("journal = (%q, %q), want option 2 label via chat", label, via)
	}
}

func TestAnswerPermission_MinusOneSendsEscape(t *testing.T) {
	m, st, rt, id := seedPermissionQuiz(t, "permission-exit-plan.pane")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{-1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sent := sentKeysEventually(rt, 1)
	if len(sent) != 1 || sent[0] != (sentKey{handle: "sess1", key: "Escape"}) {
		t.Fatalf("sent = %+v, want exactly Escape", sent)
	}
	if label, _ := answerLabelOf(t, st, id); label != "Esc" {
		t.Errorf("label = %q, want Esc", label)
	}
}

func TestAnswerPermission_InvalidAnswers(t *testing.T) {
	cases := map[string][]QuizAnswer{
		"text":              {{QuestionIndex: 0, Text: "do it differently"}},
		"two options":       {{QuestionIndex: 0, OptionIndices: []int{0, 1}}},
		"no option":         {{QuestionIndex: 0}},
		"out of range":      {{QuestionIndex: 0, OptionIndices: []int{3}}},
		"below -1":          {{QuestionIndex: 0, OptionIndices: []int{-2}}},
		"question index 1":  {{QuestionIndex: 1, OptionIndices: []int{0}}},
		"two answers":       {{QuestionIndex: 0, OptionIndices: []int{0}}, {QuestionIndex: 0, OptionIndices: []int{1}}},
		"no answers at all": {},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			m, _, rt, _ := seedPermissionQuiz(t, "permission-edit-settings.pane")
			err := m.AnswerQuiz(context.Background(), "sess1", answers)
			assertValidationCode(t, err, "invalid_answer")
			if sent := sentKeysEventually(rt, 0); len(sent) != 0 {
				t.Errorf("sent %+v, want no keys", sent)
			}
			rt.mu.Lock()
			captures := len(rt.captureLines)
			rt.mu.Unlock()
			if captures != 0 {
				t.Errorf("captured %d times, want 0 for an invalid answer", captures)
			}
		})
	}
}

// TestAnswerPermission_PaneChangedIsConflictWithoutKeys covers the human
// answering in the terminal while the chat click is in flight: the agent
// moved on to the next Bash dialog (same title, different command).
func TestAnswerPermission_PaneChangedIsConflictWithoutKeys(t *testing.T) {
	m, _, rt, _ := seedPermissionQuiz(t, "permission-bash-rm.pane")
	rt.captureOut = paneFixture(t, "permission-bash-outside-cwd.pane")

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "prompt_changed")
	if sent := sentKeysEventually(rt, 0); len(sent) != 0 {
		t.Fatalf("sent %+v, want no keys", sent)
	}

	// Not left in flight: once the pane shows the dialog again, a retry goes through.
	rt.captureOut = paneFixture(t, "permission-bash-rm.pane")
	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("retry after prompt_changed: %v", err)
	}
}

func TestAnswerPermission_DialogGoneIsConflict(t *testing.T) {
	m, _, rt, _ := seedPermissionQuiz(t, "permission-bash-rm.pane")
	rt.captureOut = paneFixture(t, "permission-neg-idle.pane")

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "prompt_changed")
	if sent := sentKeysEventually(rt, 0); len(sent) != 0 {
		t.Fatalf("sent %+v, want no keys", sent)
	}
}

func TestAnswerPermission_CaptureErrorSendsNothing(t *testing.T) {
	m, _, rt, _ := seedPermissionQuiz(t, "permission-bash-rm.pane")
	rt.captureErr = errors.New("tmux gone")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err == nil {
		t.Fatal("AnswerQuiz succeeded with a failing capture")
	}
	if sent := sentKeysEventually(rt, 0); len(sent) != 0 {
		t.Fatalf("sent %+v, want no keys", sent)
	}
}

// TestAnswerPermission_RecapturesAtSharedDepth pins that the pre-press
// re-read uses runtime.PermissionCaptureLines, the depth the monitor read
// the dialog at; another depth can yield a different Context for the same
// dialog and a false prompt_changed.
func TestAnswerPermission_RecapturesAtSharedDepth(t *testing.T) {
	m, _, rt, _ := seedPermissionQuiz(t, "permission-bash-multiline.pane")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	rt.mu.Lock()
	lines := append([]int(nil), rt.captureLines...)
	rt.mu.Unlock()
	if len(lines) != 1 || lines[0] != runtime.PermissionCaptureLines {
		t.Errorf("capture depths = %v, want [%d]", lines, runtime.PermissionCaptureLines)
	}
}

func TestAnswerPermission_SendFailureWithdrawsJournalLabel(t *testing.T) {
	m, st, rt, id := seedPermissionQuiz(t, "permission-bash-rm.pane")
	rt.sendErr = errors.New("send-keys failed")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sentKeysEventually(rt, 1)
	time.Sleep(20 * time.Millisecond)
	if label, via := answerLabelOf(t, st, id); label != "" || via != "terminal" {
		t.Errorf("journal = (%q, %q), want the chat mark withdrawn", label, via)
	}
}
