package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// QuizSourcePermission marks a pending quiz built from a Claude Code
// permission dialog read off the pane, as opposed to an AskUserQuestion quiz
// reported by the hooks (no source). The monitor owns its lifecycle, and
// AnswerQuiz answers it with a single keypress.
const QuizSourcePermission = "permission"

// permissionQuizHeader is the question header clients show as the badge.
const permissionQuizHeader = "Разрешение"

// PermissionRef is the internal part of a permission quiz: which
// permission_prompts row it is, and the dialog identity (Title + Context)
// the answer path re-checks the pane against. The public API never exposes
// it.
type PermissionRef struct {
	PromptID int64  `json:"prompt_id"`
	Title    string `json:"title"`
	Context  string `json:"context"`
}

// NewPermissionQuiz builds the pending quiz for dialog p journalled as row
// promptID: one single-select question «Title\n\nContext» whose options are
// the dialog's choices in order, so option i is answered by digit i+1.
func NewPermissionQuiz(p runtime.PermissionPrompt, promptID, askedAt int64) Quiz {
	question := p.Title
	if p.Context != "" {
		question += "\n\n" + p.Context
	}
	options := make([]QuizOption, len(p.Options))
	for i, label := range p.Options {
		options[i] = QuizOption{Label: label}
	}
	return Quiz{
		Questions: []QuizQuestion{{
			Question: question,
			Header:   permissionQuizHeader,
			Options:  options,
		}},
		AskedAt:    askedAt,
		Source:     QuizSourcePermission,
		Raw:        p.Raw,
		Permission: &PermissionRef{PromptID: promptID, Title: p.Title, Context: p.Context},
	}
}

// ParseQuiz decodes a session's stored pending quiz; ok is false when there
// is none or it does not parse.
func ParseQuiz(pendingQuiz string) (Quiz, bool) {
	if pendingQuiz == "" {
		return Quiz{}, false
	}
	var q Quiz
	if err := json.Unmarshal([]byte(pendingQuiz), &q); err != nil {
		return Quiz{}, false
	}
	return q, true
}

// IsPermission reports whether q was built from a permission dialog.
func (q Quiz) IsPermission() bool {
	return q.Source == QuizSourcePermission && q.Permission != nil
}

// PermissionPrompt returns the identity of the dialog q was built from, for
// runtime.PermissionPrompt.SameDialog. Zero for a hook quiz.
func (q Quiz) PermissionPrompt() runtime.PermissionPrompt {
	if q.Permission == nil {
		return runtime.PermissionPrompt{}
	}
	return runtime.PermissionPrompt{Title: q.Permission.Title, Context: q.Permission.Context}
}

// permissionEscIndex is the option index that answers a permission dialog
// with Escape: it cancels a tool dialog (interrupting the turn) and rejects
// a plan, the only way to refuse one.
const permissionEscIndex = -1

// permissionEscLabel is what the journal records for an Escape answer.
const permissionEscLabel = "Esc"

// permissionMaxDigitIndex is the highest option index answerable by one
// digit key (option 9).
const permissionMaxDigitIndex = 8

// validatePermissionAnswer checks answers for a permission quiz: exactly one
// answer to question 0 with exactly one option index, which is -1 (Escape)
// or a choice reachable by a single digit. Free text is refused — a
// permission dialog has no text field, and typing into one would land in
// the agent's composer. Returns the chosen option index.
func validatePermissionAnswer(quiz Quiz, answers []QuizAnswer) (int, error) {
	if len(answers) != 1 {
		return 0, fmt.Errorf("a permission prompt takes exactly one answer, got %d", len(answers))
	}
	a := answers[0]
	if a.QuestionIndex != 0 {
		return 0, fmt.Errorf("question_index %d out of range [0,1)", a.QuestionIndex)
	}
	if a.Text != "" {
		return 0, errors.New("a permission prompt takes no text answer — answer with an option, then write to the agent as a normal message")
	}
	if len(a.OptionIndices) != 1 {
		return 0, fmt.Errorf("a permission prompt takes exactly one option_index, got %d", len(a.OptionIndices))
	}
	i := a.OptionIndices[0]
	n := len(quiz.Questions[0].Options)
	if i == permissionEscIndex {
		return i, nil
	}
	if i < 0 || i >= n {
		return 0, fmt.Errorf("option_index %d out of range: -1 (Esc) or [0,%d)", i, n)
	}
	if i > permissionMaxDigitIndex {
		return 0, fmt.Errorf("option_index %d cannot be typed as a single digit", i)
	}
	return i, nil
}

// answerPermission answers a pending permission quiz with one keypress:
// digit i+1 for option i — the dialog confirms on the digit itself, so no
// Enter follows (it would submit whatever sits in the composer) — or
// Escape for -1.
//
// Right before pressing, the pane is read again at the monitor's depth
// (runtime.PermissionCaptureLines) and must still show the same dialog;
// otherwise nothing is sent and "prompt_changed" (409) is returned — the
// human may have answered in the terminal meanwhile, and a digit sent now
// would land in the next dialog or in the agent's composer. The chosen label
// is recorded on the journal row so the monitor closes it as answered from
// chat. Completion is confirmed by the monitor's session.quiz_resolved,
// through the same in-flight / unconfirmed machinery as a hook quiz.
func (m *Manager) answerPermission(ctx context.Context, sess store.Session, quiz Quiz, answers []QuizAnswer) error {
	if len(quiz.Questions) != 1 {
		return fmt.Errorf("permission quiz for session %s has %d questions, want 1", sess.ID, len(quiz.Questions))
	}
	idx, err := validatePermissionAnswer(quiz, answers)
	if err != nil {
		return validationErr("invalid_answer", err.Error())
	}

	if !m.tryStartQuizInFlight(sess.ID) {
		return validationErr("quiz_answer_in_flight", "an answer is already being sent for session: "+sess.ID)
	}
	started := false
	defer func() {
		if !started {
			m.clearQuizInFlight(sess.ID)
		}
	}()

	h := runtime.Handle{Name: sess.TmuxName}
	pane, err := m.rt.Capture(ctx, h, runtime.PermissionCaptureLines)
	if err != nil {
		return fmt.Errorf("re-read pane before answering permission prompt: %w", err)
	}
	if p, ok := runtime.ParsePermissionPrompt(pane); !ok || !p.SameDialog(quiz.PermissionPrompt()) {
		return validationErr("prompt_changed", "the permission dialog on the pane changed or closed; re-read the session")
	}

	step := keyStep{Kind: keyEscape, Settle: quizKeySettle}
	label := permissionEscLabel
	if idx != permissionEscIndex {
		step = keyStep{Kind: keyDigit, Value: strconv.Itoa(idx + 1), Settle: quizKeySettle}
		label = quiz.Questions[0].Options[idx].Label
	}

	promptID := quiz.Permission.PromptID
	if err := m.st.MarkPermissionPromptAnswered(promptID, label); err != nil {
		return err
	}
	withdraw := func() {
		if err := m.st.MarkPermissionPromptAnswered(promptID, ""); err != nil {
			slog.Default().Warn("permission answer: withdraw journal label", "session", sess.ID, "error", err)
		}
	}

	sent := func() {
		if err := m.st.MarkPermissionPromptSent(promptID, time.Now().Unix()); err != nil {
			slog.Default().Warn("permission answer: record keypress", "session", sess.ID, "error", err)
		}
	}

	started = true
	go m.runQuizAnswer(sess.ID, h, []keyStep{step}, sent, withdraw)
	return nil
}

// clearPendingForTerminal drops what a session that reached a terminal state
// can no longer answer: its pending quiz and any permission dialog still
// open in the journal (closed as terminal, so it shows in the chat feed
// rather than staying open forever). Best-effort, like the state change it
// accompanies.
func (m *Manager) clearPendingForTerminal(id string) {
	_ = m.st.ClearPendingQuiz(id)
	if err := m.st.ResolveOpenPermissionPrompts(id, time.Now().Unix()); err != nil {
		slog.Default().Warn("close open permission prompts", "session", id, "error", err)
	}
}
