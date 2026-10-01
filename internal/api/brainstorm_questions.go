// Brainstorm threads (task #4901, spec §2.2): a decision thread of an
// orchestrator's brainstorm that also records the asker's recommendation and
// how the human's answer related to it. This file holds what only brainstorm
// threads have — the wire fields, the recommendation check, the terminal
// record and the outcome override; everything else is the ordinary thread
// machinery in questions.go and threads.go.
package api

import (
	"fmt"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// brainstormWire is the part of a thread's JSON that reports a brainstorm
// answer. It is present on every thread so clients read one shape: on other
// thread types the options are null and the strings empty. AnsweredBy names
// who closed the thread with an answer ("human", an agent id, or "" while
// unanswered) on every thread type, so the brainstorm metric can count the
// human's answers only.
type brainstormWire struct {
	RecommendedOption *int   `json:"recommended_option"`
	ChosenOption      *int   `json:"chosen_option"`
	AnswerComment     string `json:"answer_comment"`
	AnswerSource      string `json:"answer_source"`
	Outcome           string `json:"outcome"`
	OutcomeOverridden bool   `json:"outcome_overridden"`
	AnsweredBy        string `json:"answered_by"`
}

// optionRef renders a 1-based option number for the wire; 0 is null.
func optionRef(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// toBrainstormWire builds the wire fields of q. answerAuthor is the author of
// q's latest answer entry, "" when it has none; it only counts while the
// thread actually stands answered — a reopened thread is unanswered again.
func toBrainstormWire(q store.Question, answerAuthor string) brainstormWire {
	w := brainstormWire{
		RecommendedOption: optionRef(q.RecommendedOption),
		ChosenOption:      optionRef(q.ChosenOption),
		AnswerComment:     q.AnswerComment,
		AnswerSource:      q.AnswerSource,
		Outcome:           q.Outcome,
		OutcomeOverridden: q.OutcomeOverridden,
	}
	if q.Status == "resolved" && q.Resolution == "answered" && answerAuthor != "" {
		w.AnsweredBy = wireParticipant(answerAuthor)
	}
	return w
}

// latestAnswerAuthor returns the author of the last answer entry in msgs, or
// "" when there is none.
func latestAnswerAuthor(msgs []store.QuestionMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == "answer" {
			return msgs[i].Author
		}
	}
	return ""
}

// validateRecommend checks a new thread's recommendation, writing a 400 and
// returning false when it is wrong. A brainstorm thread with options must
// recommend one of them; one without options has nothing to recommend; and no
// other thread type takes a recommendation at all — silently dropping it would
// hide a mistyped --brainstorm.
func validateRecommend(w http.ResponseWriter, threadType string, options []string, recommend int) bool {
	var msg string
	switch {
	case threadType != store.QuestionTypeBrainstorm && recommend != 0:
		msg = "recommend is only accepted on a brainstorm thread"
	case threadType == store.QuestionTypeBrainstorm && len(options) == 0 && recommend != 0:
		msg = "recommend needs options to point at"
	case threadType == store.QuestionTypeBrainstorm && len(options) > 0 && (recommend < 1 || recommend > len(options)):
		msg = fmt.Sprintf("a brainstorm thread with options must recommend one: recommend must be between 1 and %d", len(options))
	default:
		return true
	}
	writeErr(w, http.StatusBadRequest, "bad_request", msg)
	return false
}
