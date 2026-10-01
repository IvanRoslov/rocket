package store

import "fmt"

// ReopenQuestion flips a resolved question back to open, clearing its
// resolution and resolved_at. Used when the task's orchestrator disputes
// the human's final answer by replying into the resolved thread (see
// internal/api's handlePostQuestionReply): the disagreement continues in
// the SAME thread instead of spawning a disconnected new question. The
// answer text itself stays in the thread history as a message. An fyi thread
// reopened this way becomes an ordinary decision thread — somebody did care
// about the status note after all. A brainstorm thread stays brainstorm, but
// its recorded answer and outcome are cleared: the next answer computes them
// afresh. Returns
// ErrNotFound for an unknown id and ErrQuestionOpen if the question is not
// resolved.
func (s *Store) ReopenQuestion(id int64) error {
	res, err := s.db.Exec(
		`UPDATE questions SET status = 'open', resolution = '', resolved_at = NULL,
		        type = CASE WHEN type = 'fyi' THEN 'decision' ELSE type END,
		        chosen_option = NULL, answer_comment = '', answer_source = '',
		        outcome = '', outcome_overridden = 0
		 WHERE id = ? AND status = 'resolved'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("reopen question: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reopen question rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}
	if _, err := s.GetQuestion(id); err != nil {
		return err
	}
	return ErrQuestionOpen
}
