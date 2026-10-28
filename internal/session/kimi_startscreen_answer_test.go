package session

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
)

// kimiTrustCursorOnExit is the Kimi trust screen with the ❯ cursor moved to
// «Exit».
func kimiTrustCursorOnExit(t *testing.T) string {
	t.Helper()
	p := paneFixture(t, "kimi-trust.pane")
	p = strings.Replace(p, "   ❯ Trust and continue", "     Trust and continue", 1)
	return strings.Replace(p, "     Exit", "   ❯ Exit", 1)
}

// seedKimiStartScreenQuiz puts sess1 on Kimi start screen pane.
func seedKimiStartScreenQuiz(t *testing.T, pane string) (*Manager, *fakeRuntime) {
	t.Helper()
	m, st, _, rt, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	p, ok := runtime.ParseKimiStartScreen(pane)
	if !ok {
		t.Fatal("pane not parsed")
	}
	id, _, err := st.OpenPermissionPrompt("sess1", p.Title, p.Context, "[]", 1)
	if err != nil {
		t.Fatalf("OpenPermissionPrompt: %v", err)
	}
	b, _ := json.Marshal(NewKimiStartScreenQuiz(p, id, 1))
	if err := st.SetPendingQuiz("sess1", string(b)); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
	rt.captureOut = pane
	return m, rt
}

func keysOf(sent []sentKey) []string {
	var out []string
	for _, s := range sent {
		out = append(out, s.key)
	}
	return out
}

func TestNewKimiStartScreenQuizIsUnnumbered(t *testing.T) {
	p, _ := runtime.ParseKimiStartScreen(paneFixture(t, "kimi-trust.pane"))
	q := NewKimiStartScreenQuiz(p, 7, 1)
	if !q.IsStartScreen() || !q.Permission.Unnumbered || q.Questions[0].Header != "Экран Kimi Code" {
		t.Errorf("quiz = %+v", q)
	}
	if NewStartScreenQuiz(p, 7, 1).Permission.Unnumbered {
		t.Error("Codex start-screen quiz flagged unnumbered")
	}
}

func TestAnswerKimiStartScreenArrowsThenEnter(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, paneFixture(t, "kimi-trust.pane"))
	rt.captureAfterKeys = kimiTrustCursorOnExit(t)
	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	if got := keysOf(sentKeysEventually(rt, 2)); !reflect.DeepEqual(got, []string{"Down", "Enter"}) {
		t.Fatalf("keys = %q, want Down, Enter", got)
	}
}

func TestAnswerKimiStartScreenCursorAlreadyThere(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, paneFixture(t, "kimi-trust.pane"))
	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	if got := keysOf(sentKeysEventually(rt, 1)); !reflect.DeepEqual(got, []string{"Enter"}) {
		t.Fatalf("keys = %q, want Enter", got)
	}
}

func TestAnswerKimiStartScreenUp(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, kimiTrustCursorOnExit(t))
	rt.captureAfterKeys = paneFixture(t, "kimi-trust.pane")
	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	if got := keysOf(sentKeysEventually(rt, 2)); !reflect.DeepEqual(got, []string{"Up", "Enter"}) {
		t.Fatalf("keys = %q, want Up, Enter", got)
	}
}

// The arrow did not move the cursor onto the choice: no Enter, which would
// confirm the wrong option.
func TestAnswerKimiStartScreenNoEnterWhenCursorElsewhere(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, paneFixture(t, "kimi-trust.pane"))
	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}
	if got := keysOf(sentKeysEventually(rt, 2)); !reflect.DeepEqual(got, []string{"Down"}) {
		t.Fatalf("keys = %q, want only Down", got)
	}
}

func TestAnswerKimiStartScreenPromptChanged(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, paneFixture(t, "kimi-trust.pane"))
	rt.captureOut = paneFixture(t, "kimi-idle.pane")
	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}})
	assertValidationCode(t, err, "prompt_changed")
	if n := len(sentKeysEventually(rt, 1)); n != 0 {
		t.Fatalf("%d keys sent on a changed screen", n)
	}
}

// Esc on Kimi's trust screen exits Kimi: not offered for unnumbered screens.
func TestAnswerKimiStartScreenRejectsEsc(t *testing.T) {
	m, rt := seedKimiStartScreenQuiz(t, paneFixture(t, "kimi-trust.pane"))
	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{-1}}})
	assertValidationCode(t, err, "invalid_answer")
	if n := len(sentKeysEventually(rt, 1)); n != 0 {
		t.Fatalf("%d keys sent", n)
	}
}
