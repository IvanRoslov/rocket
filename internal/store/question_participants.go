package store

import (
	"fmt"
	"time"
)

// ListParticipants returns the participant ids of a thread, sorted by id so
// callers and tests see a deterministic order.
func (s *Store) ListParticipants(questionID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT participant_id FROM question_participants
		 WHERE question_id = ? ORDER BY participant_id`, questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query question participants: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan question participant: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AddParticipants adds ids to a thread's participant list. It is idempotent:
// an id already present is left alone rather than reported as an error, which
// is what makes it safe to call from every ask/reply/answer path and safe to
// race against itself — the UNIQUE (question_id, participant_id) constraint
// plus INSERT OR IGNORE resolves concurrent inserts in the database. Passing
// no ids is a no-op.
func (s *Store) AddParticipants(questionID int64, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin add participants: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	for _, id := range ids {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO question_participants (question_id, participant_id, added_at)
			 VALUES (?, ?, ?)`,
			questionID, canonicalParticipant(id), now,
		); err != nil {
			return fmt.Errorf("insert question participant %q: %w", id, err)
		}
	}
	return tx.Commit()
}

// ListQuestionsForParticipant returns the threads participantID takes part in,
// ascending by id. If openOnly is true only open threads are returned.
func (s *Store) ListQuestionsForParticipant(participantID string, openOnly bool) ([]Question, error) {
	query := `SELECT ` + questionColumns + ` FROM questions q
	          WHERE EXISTS (SELECT 1 FROM question_participants p
	                        WHERE p.question_id = q.id AND p.participant_id = ?)`
	if openOnly {
		query += ` AND q.status = 'open'`
	}
	query += ` ORDER BY q.id`

	rows, err := s.db.Query(query, canonicalParticipant(participantID))
	if err != nil {
		return nil, fmt.Errorf("query questions for participant: %w", err)
	}
	defer rows.Close()

	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
