package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
)

// seedStartScreenQuiz puts sess1 on the Codex start screen of fixture.
func seedStartScreenQuiz(t *testing.T, fixture string) (*Manager, *fakeRuntime) {
	t.Helper()
	m, st, _, rt, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	pane := paneFixture(t, fixture)
	p, ok := runtime.ParseStartScreen(pane)
	if !ok {
		t.Fatalf("fixture %s not parsed", fixture)
	}
	id, _, err := st.OpenPermissionPrompt("sess1", p.Title, p.Context, "[]", 1)
	if err != nil {
		t.Fatalf("OpenPermissionPrompt: %v", err)
	}
	b, _ := json.Marshal(NewStartScreenQuiz(p, id, 1))
	if err := st.SetPendingQuiz("sess1", string(b)); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
	rt.captureOut = pane
	return m, rt
}

func TestNewStartScreenQuizIsFlagged(t *testing.T) {
	p, _ := runtime.ParseStartScreen(paneFixture(t, "codex-start-trust.pane"))
	q := NewStartScreenQuiz(p, 7, 1)
	if !q.IsPermission() || !q.IsStartScreen() || q.Permission.PromptID != 7 {
		t.Errorf("quiz = %+v, want a start-screen permission quiz", q)
	}
	if got := len(q.Questions[0].Options); got != 2 {
		t.Errorf("options = %d, want 2", got)
	}
	if NewPermissionQuiz(p, 7, 1).IsStartScreen() {
		t.Error("a plain permission quiz reports IsStartScreen")
	}
}

// Trust: the digit only moves Codex's cursor, so the screen stays with the
// cursor on the chosen row and Enter confirms it.
func TestAnswerStartScreenSendsEnterWhenScreenStays(t *testing.T) {
	m, rt := seedStartScreenQuiz(t, "codex-start-trust.pane")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sent := sentKeysEventually(rt, 2)
	want := []sentKey{{handle: "sess1", key: "1"}, {handle: "sess1", key: "Enter"}}
	if len(sent) != 2 || sent[0] != want[0] || sent[1] != want[1] {
		t.Fatalf("sent = %+v, want %+v", sent, want)
	}
}

// Update: the digit selects at once and the screen goes away — no Enter,
// which would land on whatever comes next.
func TestAnswerStartScreenNoEnterWhenScreenGone(t *testing.T) {
	m, rt := seedStartScreenQuiz(t, "codex-start-update.pane")
	rt.captureAfterKeys = paneFixture(t, "codex-start-neg-idle.pane")

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{2}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sent := sentKeysEventually(rt, 2)
	if len(sent) != 1 || sent[0] != (sentKey{handle: "sess1", key: "3"}) {
		t.Fatalf("sent = %+v, want exactly digit 3", sent)
	}
}

// The screen still shows but the cursor is elsewhere (the digit did not
// land): no Enter, which would confirm the wrong choice.
func TestAnswerStartScreenNoEnterWhenCursorElsewhere(t *testing.T) {
	m, rt := seedStartScreenQuiz(t, "codex-start-daemon.pane") // cursor on 3. Cancel

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	sent := sentKeysEventually(rt, 2)
	if len(sent) != 1 || sent[0] != (sentKey{handle: "sess1", key: "2"}) {
		t.Fatalf("sent = %+v, want exactly digit 2", sent)
	}
}

func TestAnswerStartScreenPromptChanged(t *testing.T) {
	m, rt := seedStartScreenQuiz(t, "codex-start-trust.pane")
	rt.captureOut = paneFixture(t, "codex-start-update.pane")

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "prompt_changed")
}
