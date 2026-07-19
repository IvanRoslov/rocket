package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// QuizOption is a single selectable option in a quiz question, mirroring
// AskUserQuestion's option shape as delivered by the PreToolUse hook (see
// internal/api's internal_quiz.go).
type QuizOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuizQuestion is one question in a pending quiz, as stored (verbatim from
// tool_input.questions) on Session.PendingQuiz.
type QuizQuestion struct {
	Question    string       `json:"question"`
	Header      string       `json:"header"`
	MultiSelect bool         `json:"multiSelect"`
	Options     []QuizOption `json:"options"`
}

// Quiz is the full pending-quiz payload stored on Session.PendingQuiz:
// {"questions":[...],"asked_at":<unix>}.
type Quiz struct {
	Questions []QuizQuestion `json:"questions"`
	AskedAt   int64          `json:"asked_at"`
}

// QuizAnswer is one answer in a POST /v1/sessions/{id}/quiz/answer request:
// either OptionIndices (selecting from the question's declared options —
// exactly one for a single-select question, one or more for multi-select)
// or Text (the "Other"/free-text row), never both.
type QuizAnswer struct {
	QuestionIndex int    `json:"question_index"`
	OptionIndices []int  `json:"option_indices,omitempty"`
	Text          string `json:"text,omitempty"`
}

// quizKeyKind is the vocabulary of atomic tmux key-presses the quiz
// injector can emit. Space is part of the vocabulary (Claude Code's TUI
// accepts it as an alternate multi-select toggle, per
// docs/superpowers/recon/2026-07-19-quiz-recon2-hooks.md §4) even though
// quizKeySequence currently only ever emits digit toggles, which recon
// confirmed equally reliable and simpler to reason about (no cursor
// tracking needed).
type quizKeyKind string

const (
	keyDigit   quizKeyKind = "digit"
	keySpace   quizKeyKind = "space"
	keyTab     quizKeyKind = "tab"
	keyDown    quizKeyKind = "down"
	keyLiteral quizKeyKind = "literal"
	keyEnter   quizKeyKind = "enter"
)

// quizKeySettle is the pause after each keystroke, letting Claude Code's
// TUI redraw before the next one lands. Recon
// (docs/superpowers/recon/2026-07-19-quiz-recon.md §2) found 0.3-1s
// sufficient; 300ms is the fixed constant per the design spec.
const quizKeySettle = 300 * time.Millisecond

// keyStep is one atomic step in a quiz-answer keystroke sequence: a key
// (Value holds the digit for keyDigit or the text for keyLiteral; unused
// for the other kinds) plus the settle pause to wait after sending it.
type keyStep struct {
	Kind   quizKeyKind
	Value  string
	Settle time.Duration
}

// validateQuizAnswers checks answers against quiz, returning a descriptive
// error covering every case POST /v1/sessions/{id}/quiz/answer must reject
// with 400 quiz_answer_invalid: a question_index out of range, a
// single-select answer without exactly one option_index, both
// option_indices and text supplied together, empty/whitespace-only text,
// an option_index out of range for its question, a duplicate answer for
// the same question, or not every question answered.
func validateQuizAnswers(quiz Quiz, answers []QuizAnswer) error {
	n := len(quiz.Questions)
	seen := make(map[int]bool, n)

	for _, a := range answers {
		if a.QuestionIndex < 0 || a.QuestionIndex >= n {
			return fmt.Errorf("question_index %d out of range [0,%d)", a.QuestionIndex, n)
		}
		if seen[a.QuestionIndex] {
			return fmt.Errorf("duplicate answer for question_index %d", a.QuestionIndex)
		}
		seen[a.QuestionIndex] = true

		hasOptions := len(a.OptionIndices) > 0
		hasText := a.Text != ""
		if hasOptions && hasText {
			return fmt.Errorf("question_index %d: option_indices and text are mutually exclusive", a.QuestionIndex)
		}
		if !hasOptions && !hasText {
			return fmt.Errorf("question_index %d: must provide option_indices or text", a.QuestionIndex)
		}
		if hasText && strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("question_index %d: text must not be empty", a.QuestionIndex)
		}
		if hasOptions {
			q := quiz.Questions[a.QuestionIndex]
			if !q.MultiSelect && len(a.OptionIndices) != 1 {
				return fmt.Errorf("question_index %d: single-select requires exactly one option_index, got %d", a.QuestionIndex, len(a.OptionIndices))
			}
			for _, oi := range a.OptionIndices {
				if oi < 0 || oi >= len(q.Options) {
					return fmt.Errorf("question_index %d: option_index %d out of range [0,%d)", a.QuestionIndex, oi, len(q.Options))
				}
			}
		}
	}

	if len(seen) != n {
		return fmt.Errorf("all %d questions must be answered, got %d", n, len(seen))
	}
	return nil
}

// quizKeySequence is the pure, tmux-free core of the quiz-answer injector:
// it translates a validated (quiz, answers) pair into the exact ordered
// sequence of keystrokes docs/superpowers/specs/2026-07-19-remote-quiz-design.md
// §4 describes for driving Claude Code's AskUserQuestion TUI, one question
// at a time in ascending question_index order (which recon confirmed
// matches the TUI's left-to-right tab order):
//
//   - single-select: one digit selecting the chosen option (1-indexed) —
//     the TUI auto-advances to the next unanswered tab, or Submit if it
//     was the last question.
//   - multi-select: one digit per selected option (ascending, each toggles
//     a checkbox without advancing), followed by Tab to leave the tab.
//   - free text ("Other"): Down repeated len(options) times to reach the
//     "Type something." row (never a digit — a digit on an option row
//     selects immediately, which would misfire), then the literal text,
//     then Enter (which both submits the free-text answer and, per recon,
//     advances like a digit selection would).
//
// Finally, a single Enter confirms the "1. Submit answers" default choice
// on the review screen every question's last step is expected to land on.
//
// quizKeySequence re-validates via validateQuizAnswers before building
// anything, so it can never itself be the source of the one inviolable
// invariant it must uphold: it must never emit an "enter" step immediately
// after navigating to the free-text row without a non-empty "literal" step
// in between — a bare Enter there cancels the whole quiz instead of
// confirming an empty answer (see the recon "Other-row trap").
func quizKeySequence(quiz Quiz, answers []QuizAnswer) ([]keyStep, error) {
	if err := validateQuizAnswers(quiz, answers); err != nil {
		return nil, err
	}

	byIndex := make(map[int]QuizAnswer, len(answers))
	for _, a := range answers {
		byIndex[a.QuestionIndex] = a
	}

	var steps []keyStep
	for i, q := range quiz.Questions {
		a := byIndex[i]

		switch {
		case a.Text != "":
			for range q.Options {
				steps = append(steps, keyStep{Kind: keyDown, Settle: quizKeySettle})
			}
			steps = append(steps, keyStep{Kind: keyLiteral, Value: a.Text, Settle: quizKeySettle})
			steps = append(steps, keyStep{Kind: keyEnter, Settle: quizKeySettle})

		case q.MultiSelect:
			idx := append([]int(nil), a.OptionIndices...)
			sort.Ints(idx)
			for _, oi := range idx {
				steps = append(steps, keyStep{Kind: keyDigit, Value: strconv.Itoa(oi + 1), Settle: quizKeySettle})
			}
			steps = append(steps, keyStep{Kind: keyTab, Settle: quizKeySettle})

		default:
			steps = append(steps, keyStep{Kind: keyDigit, Value: strconv.Itoa(a.OptionIndices[0] + 1), Settle: quizKeySettle})
		}
	}

	// Final confirmation on the review screen.
	steps = append(steps, keyStep{Kind: keyEnter, Settle: quizKeySettle})

	return steps, nil
}

// quizKeyName maps a keyStep to the tmux send-keys key-name argument used
// when it is not sent as literal text (see sendQuizKeys).
func quizKeyName(s keyStep) string {
	switch s.Kind {
	case keyDigit:
		return s.Value
	case keySpace:
		return "Space"
	case keyTab:
		return "Tab"
	case keyDown:
		return "Down"
	case keyEnter:
		return "Enter"
	default:
		return s.Value
	}
}

// sendQuizKeys executes steps against h via m.rt.SendKeys, pausing
// m.quizSleepFn(step.Settle) after each one (see quizKeySettle). It stops
// and returns the first error encountered.
func (m *Manager) sendQuizKeys(ctx context.Context, h runtime.Handle, steps []keyStep) error {
	for _, s := range steps {
		var err error
		if s.Kind == keyLiteral {
			err = m.rt.SendKeys(ctx, h, s.Value, true)
		} else {
			err = m.rt.SendKeys(ctx, h, quizKeyName(s), false)
		}
		if err != nil {
			return fmt.Errorf("send quiz key %q: %w", s.Kind, err)
		}
		m.quizSleepFn(s.Settle)
	}
	return nil
}

// AnswerQuiz validates answers against session id's pending quiz and, on
// success, asynchronously drives Claude Code's AskUserQuestion TUI via
// tmux keystrokes (see quizKeySequence), returning as soon as the
// keystroke sequence has been built and handed off — callers (the
// POST /v1/sessions/{id}/quiz/answer handler) should respond 202
// "answering" immediately after AnswerQuiz returns nil, since actual
// completion is only ever confirmed out-of-band by the resolved hook (see
// watchQuizUnconfirmed).
//
// Returns *ValidationError with code "session_not_found" (404),
// "no_pending_quiz" (409, the session has nothing to answer — e.g. a
// human already answered in the terminal, or a stale/duplicate request),
// or "quiz_answer_invalid" (400, answers fails validateQuizAnswers).
func (m *Manager) AnswerQuiz(ctx context.Context, id string, answers []QuizAnswer) error {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}

	if sess.PendingQuiz == "" {
		return validationErr("no_pending_quiz", "session has no pending quiz")
	}

	var quiz Quiz
	if err := json.Unmarshal([]byte(sess.PendingQuiz), &quiz); err != nil {
		return fmt.Errorf("parse pending quiz for session %s: %w", id, err)
	}

	steps, err := quizKeySequence(quiz, answers)
	if err != nil {
		return validationErr("quiz_answer_invalid", err.Error())
	}

	h := runtime.Handle{Name: sess.TmuxName}
	go m.runQuizAnswer(id, h, steps)

	return nil
}

// runQuizAnswer sends steps to h (best-effort — a failure here is logged
// but does not stop the unconfirmed watch, since even a partially-failed
// injection may have already changed on-screen state a human now needs to
// resolve) and then watches for the resolved hook, warning if it never
// arrives.
func (m *Manager) runQuizAnswer(id string, h runtime.Handle, steps []keyStep) {
	if err := m.sendQuizKeys(context.Background(), h, steps); err != nil {
		slog.Default().Warn("quiz answer: keystroke injection failed", "session", id, "error", err)
	}
	m.watchQuizUnconfirmed(id)
}

// watchQuizUnconfirmed subscribes to the bus and waits, up to
// m.quizUnconfirmedTimeout, for a session.quiz_resolved event naming id —
// the PostToolUse hook firing after the quiz is actually answered/declined
// in the TUI (see internal/api's internal_quiz.go). If the timeout elapses
// first, it publishes session.quiz_answer_unconfirmed{session_id:id}
// (warn-logged) and returns without touching the session's pending_quiz —
// per the design spec, only the resolved hook is authoritative for
// clearing it.
func (m *Manager) watchQuizUnconfirmed(id string) {
	ch, cancel := m.bus.Subscribe()
	defer cancel()

	timer := time.NewTimer(m.quizUnconfirmedTimeout)
	defer timer.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Type == "session.quiz_resolved" && ev.SessionID == id {
				return
			}
		case <-timer.C:
			slog.Default().Warn("quiz answer unconfirmed", "session", id, "timeout", m.quizUnconfirmedTimeout)
			m.bus.Publish("session.quiz_answer_unconfirmed", id, map[string]any{})
			return
		}
	}
}

// SetQuizTiming overrides the quiz-answer injector's settle-pause function
// and/or unconfirmed-watch timeout. Intended for tests: a nil sleep or a
// zero/negative timeout leaves the corresponding current value unchanged.
// Production code never needs to call this — NewManager's defaults
// (time.Sleep, 60s) match the design spec.
func (m *Manager) SetQuizTiming(sleep func(time.Duration), unconfirmedTimeout time.Duration) {
	if sleep != nil {
		m.quizSleepFn = sleep
	}
	if unconfirmedTimeout > 0 {
		m.quizUnconfirmedTimeout = unconfirmedTimeout
	}
}
