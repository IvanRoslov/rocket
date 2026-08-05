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

// ListOpenThreads returns every OPEN thread bound to a task or a role — the
// aggregate the board, task list and agent list all annotate from.
func (s *Store) ListOpenThreads() ([]OpenThread, error) {
	return s.ListThreads(false)
}

// ListThreads returns every thread bound to a task or a role, ascending by
// question id, in a fixed number of queries regardless of how many threads
// there are. includeResolved widens the listing to closed threads, which the
// unified inbox needs for "rocket questions --all"; a closed thread carries no
// attention, so its set comes back empty either way.
func (s *Store) ListThreads(includeResolved bool) ([]OpenThread, error) {
	// One status predicate, spelled once and reused by every query below, so a
	// widened listing can never disagree with its own participant rows.
	statusFilter := "q.status = 'open'"
	if includeResolved {
		statusFilter = "1 = 1"
	}

	rows, err := s.db.Query(`
		SELECT ` + questionColumns + `
		FROM questions q
		WHERE ` + statusFilter + ` AND (q.task_id IS NOT NULL OR q.role_id IS NOT NULL)
		ORDER BY q.id`)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	var out []OpenThread
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, OpenThread{Question: q})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}

	last, err := s.lastMessagesOfThreads(statusFilter)
	if err != nil {
		return nil, err
	}
	parts, err := s.participantsOfThreads(statusFilter)
	if err != nil {
		return nil, err
	}
	attention, err := s.AttentionOfOpenThreads()
	if err != nil {
		return nil, err
	}
	for i := range out {
		id := out[i].Question.ID
		out[i].LastMessage = last[id]
		out[i].Participants = parts[id]
		out[i].Attention = attention[id]
	}
	return out, nil
}

// lastMessagesOfThreads returns the newest message of every thread matching
// statusFilter, keyed by question id. Threads that nobody has spoken in yet are
// simply absent from the map.
func (s *Store) lastMessagesOfThreads(statusFilter string) (map[int64]*QuestionMessage, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.question_id, m.author, m.kind, m.body, m.addressed_to, m.created_at
		FROM question_messages m
		JOIN questions q ON q.id = m.question_id
		WHERE ` + statusFilter + `
			AND m.id = (SELECT MAX(id) FROM question_messages WHERE question_id = q.id)`)
	if err != nil {
		return nil, fmt.Errorf("query thread last messages: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]*QuestionMessage)
	for rows.Next() {
		var m QuestionMessage
		var author, kind, body, addressedTo sql.NullString
		if err := rows.Scan(&m.ID, &m.QuestionID, &author, &kind, &body,
			&addressedTo, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan thread last message: %w", err)
		}
		m.Author = canonicalParticipant(author.String)
		m.Kind = kind.String
		m.Body = body.String
		m.AddressedTo = decodeAddressedTo(addressedTo.String)
		out[m.QuestionID] = &m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// participantsOfThreads returns the participant ids of every thread matching
// statusFilter, keyed by question id and sorted within each thread, matching
// ListParticipants.
func (s *Store) participantsOfThreads(statusFilter string) (map[int64][]string, error) {
	rows, err := s.db.Query(`
		SELECT p.question_id, p.participant_id
		FROM question_participants p
		JOIN questions q ON q.id = p.question_id
		WHERE ` + statusFilter + `
		ORDER BY p.question_id, p.participant_id`)
	if err != nil {
		return nil, fmt.Errorf("query thread participants: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var qid int64
		var id string
		if err := rows.Scan(&qid, &id); err != nil {
			return nil, fmt.Errorf("scan thread participant: %w", err)
		}
		out[qid] = append(out[qid], id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
