package session

import (
	"encoding/json"

	"github.com/IvanRoslov/rocket/internal/runtime"
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
