package store

import "fmt"

// ReopenQuestion flips a resolved question back to open, clearing its
// resolution and resolved_at. Used when the task's orchestrator disputes
// the human's final answer by replying into the resolved thread (see
// internal/api's handlePostQuestionReply): the disagreement continues in
// the SAME thread instead of spawning a disconnected new question. The
// answer text itself stays in the thread history as a message. Returns
// ErrNotFound for an unknown id and ErrQuestionOpen if the question is not
// resolved.
func (s *Store) ReopenQuestion(id int64) error {
	res, err := s.db.Exec(
		`UPDATE questions SET status = 'open', resolution = '', resolved_at = NULL
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
