package store

import (
	"database/sql"
	"fmt"
)

// OpenThread is everything the turn derivation needs about one open thread:
// the question itself, its last message (nil when nothing has been said yet)
// and its participants. It exists so internal/api can compute waiting_on — and
// therefore the awaiting-user counters — for every open thread with the same
// code that renders a single thread, instead of a second derivation in SQL.
type OpenThread struct {
	Question     Question
	LastMessage  *QuestionMessage
	Participants []string
	// Attention is the stored "whose turn" set (migration 0011). Since task
	// #1023 it — not the last message's addressed_to — is what waiting_on and
	// the awaiting-user badges are read from.
	Attention []string
}

// ListOpenThreads returns every open thread bound to a task or a role,
// ascending by question id, in two queries regardless of how many threads
// there are — the aggregate the board, task list and agent list all annotate
// from.
func (s *Store) ListOpenThreads() ([]OpenThread, error) {
	rows, err := s.db.Query(`
		SELECT q.id, q.task_id, q.role_id, q.asked_by, q.body, q.context,
		       q.status, q.resolution, q.asked_at, q.resolved_at,
		       m.id, m.author, m.kind, m.body, m.addressed_to, m.created_at
		FROM questions q
		LEFT JOIN question_messages m
			ON m.question_id = q.id
			AND m.id = (SELECT MAX(id) FROM question_messages WHERE question_id = q.id)
		WHERE q.status = 'open' AND (q.task_id IS NOT NULL OR q.role_id IS NOT NULL)
		ORDER BY q.id`)
	if err != nil {
		return nil, fmt.Errorf("query open threads: %w", err)
	}
	defer rows.Close()

	var out []OpenThread
	for rows.Next() {
		var (
			q                         Question
			roleID, context, resolutn sql.NullString
			taskID, resolvedAt        sql.NullInt64
			msgID, msgCreatedAt       sql.NullInt64
			msgAuthor, msgKind        sql.NullString
			msgBody, msgAddressedTo   sql.NullString
		)
		if err := rows.Scan(
			&q.ID, &taskID, &roleID, &q.AskedBy, &q.Body, &context,
			&q.Status, &resolutn, &q.AskedAt, &resolvedAt,
			&msgID, &msgAuthor, &msgKind, &msgBody, &msgAddressedTo, &msgCreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan open thread: %w", err)
		}
		q.TaskID = taskID.Int64
		q.RoleID = roleID.String
		q.Context = context.String
		q.Resolution = resolutn.String
		q.ResolvedAt = resolvedAt.Int64

		th := OpenThread{Question: q}
		if msgID.Valid {
			th.LastMessage = &QuestionMessage{
				ID:          msgID.Int64,
				QuestionID:  q.ID,
				Author:      canonicalParticipant(msgAuthor.String),
				Kind:        msgKind.String,
				Body:        msgBody.String,
				AddressedTo: decodeAddressedTo(msgAddressedTo.String),
				CreatedAt:   msgCreatedAt.Int64,
			}
		}
		out = append(out, th)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}

	parts, err := s.participantsOfOpenThreads()
	if err != nil {
		return nil, err
	}
	attention, err := s.AttentionOfOpenThreads()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Participants = parts[out[i].Question.ID]
		out[i].Attention = attention[out[i].Question.ID]
	}
	return out, nil
}

// participantsOfOpenThreads returns the participant ids of every open thread,
// keyed by question id and sorted within each thread, matching ListParticipants.
func (s *Store) participantsOfOpenThreads() (map[int64][]string, error) {
	rows, err := s.db.Query(`
		SELECT p.question_id, p.participant_id
		FROM question_participants p
		JOIN questions q ON q.id = p.question_id
		WHERE q.status = 'open'
		ORDER BY p.question_id, p.participant_id`)
	if err != nil {
		return nil, fmt.Errorf("query open thread participants: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var qid int64
		var id string
		if err := rows.Scan(&qid, &id); err != nil {
			return nil, fmt.Errorf("scan open thread participant: %w", err)
		}
		out[qid] = append(out[qid], id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
