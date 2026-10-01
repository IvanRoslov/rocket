package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Brainstorm outcomes: how the human's answer related to the asker's
// recommendation (task #4901, spec §2.2).
const (
	OutcomeAccepted  = "accepted"
	OutcomeCorrected = "corrected"
	OutcomeWrongTurn = "wrong_turn"
)

// Where a brainstorm answer came from: the human's own click in a client, or
// the human's words in the orchestrator's terminal, recorded by the agent.
const (
	AnswerSourceUI       = "ui"
	AnswerSourceTerminal = "terminal"
)

// ValidOutcome reports whether o is one of the three brainstorm outcomes.
func ValidOutcome(o string) bool {
	return o == OutcomeAccepted || o == OutcomeCorrected || o == OutcomeWrongTurn
}

// BrainstormOutcome computes the outcome of a brainstorm answer from the
// recommended and the chosen option (both 1-based, 0 = none): picking the
// recommendation accepts it, picking another option corrects it, and writing
// one's own text instead of any option means the brainstorm took a wrong turn.
func BrainstormOutcome(recommended, chosen int) string {
	switch {
	case chosen == 0:
		return OutcomeWrongTurn
	case chosen == recommended:
		return OutcomeAccepted
	default:
		return OutcomeCorrected
	}
}

// BrainstormAnswer is the human's answer to a brainstorm thread: the chosen
// option (1-based, 0 = own text), the human's comment or own text, and the
// answer source (AnswerSourceUI or AnswerSourceTerminal).
type BrainstormAnswer struct {
	ChosenOption int
	Comment      string
	Source       string
}

// ResolveBrainstormQuestion resolves an open brainstorm thread as answered and
// records a and the computed outcome in the same statement, so a thread can
// never be resolved without its outcome or carry an outcome while open. It
// returns the outcome. ErrNotFound: no such question; ErrQuestionResolved:
// it is not an open brainstorm thread.
func (s *Store) ResolveBrainstormQuestion(id int64, a BrainstormAnswer) (string, error) {
	q, err := s.GetQuestion(id)
	if err != nil {
		return "", err
	}
	outcome := BrainstormOutcome(q.RecommendedOption, a.ChosenOption)

	res, err := s.db.Exec(
		`UPDATE questions SET status = 'resolved', resolution = 'answered', resolved_at = ?,
		        chosen_option = ?, answer_comment = ?, answer_source = ?,
		        outcome = ?, outcome_overridden = 0
		 WHERE id = ? AND status = 'open' AND type = ?`,
		time.Now().Unix(), nullIfZero(int64(a.ChosenOption)), a.Comment, a.Source,
		outcome, id, QuestionTypeBrainstorm,
	)
	if err != nil {
		return "", fmt.Errorf("resolve brainstorm question: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("resolve brainstorm question rows affected: %w", err)
	}
	if n != 1 {
		return "", ErrQuestionResolved
	}
	return outcome, nil
}

// SetQuestionOutcome overrides the outcome of a thread by hand and marks it
// overridden. Who may do this and on which threads is decided by the API.
// Returns ErrNotFound for an unknown id.
func (s *Store) SetQuestionOutcome(id int64, outcome string) error {
	res, err := s.db.Exec(
		`UPDATE questions SET outcome = ?, outcome_overridden = 1 WHERE id = ?`, outcome, id)
	if err != nil {
		return fmt.Errorf("set question outcome: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set question outcome rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AnswerAuthors maps every question that has an answer entry to the author of
// its latest one — who closed the thread with a decision, as opposed to who
// dismissed it. One query for all threads, so a listing need not read every
// thread's messages to name its answerer.
func (s *Store) AnswerAuthors() (map[int64]string, error) {
	rows, err := s.db.Query(
		`SELECT question_id, author FROM question_messages WHERE kind = 'answer' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query answer authors: %w", err)
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var qid int64
		var author sql.NullString
		if err := rows.Scan(&qid, &author); err != nil {
			return nil, fmt.Errorf("scan answer author: %w", err)
		}
		out[qid] = canonicalParticipant(author.String)
	}
	return out, rows.Err()
}
