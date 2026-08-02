package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AgentQuestion is the originating message of a role's Q&A thread. It mirrors
// Question (task threads) with the role as the counterpart instead of a task:
// AskedBy is the session id of the role instance that opened the thread, or
// "" when the human opened it.
type AgentQuestion struct {
	ID         int64
	RoleID     string
	AskedBy    string // session id of the asking role instance; "" = human
	Body       string
	Context    string // optional markdown context
	Status     string // open|resolved
	Resolution string // answered|dismissed (set once resolved)
	// AddressedTo narrows who is expected to respond before the thread has any
	// messages; see Question.AddressedTo.
	AddressedTo []string
	AskedAt     int64
	ResolvedAt  int64 // 0 = not resolved
}

// AgentQuestionMessage is a single entry in a role question's thread: either a
// reply (from either side, thread stays open) or the resolving answer (from
// the human, thread becomes resolved).
type AgentQuestionMessage struct {
	ID         int64
	QuestionID int64
	Author     string // session id of the role instance, or "" for the human
	Kind       string // reply|answer
	Body       string
	CreatedAt  int64
}

// Role threads live in the same tables as task threads since migration 0009;
// what makes a thread a role thread is a non-NULL role_id. The functions below
// are a thin facade over that shared storage, kept so internal/api's role
// handlers keep their existing shape.
const agentQuestionColumns = `id, role_id, asked_by, body, context, status, resolution, addressed_to, asked_at, resolved_at`

// toQuestion / toAgentQuestion convert between the facade's type and the
// unified one, so the facade owns no SQL of its own where it can avoid it.
func (q AgentQuestion) toQuestion() Question {
	return Question{
		ID: q.ID, RoleID: q.RoleID, AskedBy: q.AskedBy, Body: q.Body, Context: q.Context,
		Status: q.Status, Resolution: q.Resolution, AddressedTo: q.AddressedTo,
		AskedAt: q.AskedAt, ResolvedAt: q.ResolvedAt,
	}
}

// AddAgentQuestion inserts a new role question with status "open" and AskedAt
// defaulted to now (unless already set). Returns the assigned id.
func (s *Store) AddAgentQuestion(q AgentQuestion) (int64, error) {
	if q.AskedAt == 0 {
		q.AskedAt = time.Now().Unix()
	}
	if q.Status == "" {
		q.Status = "open"
	}

	return s.AddQuestion(q.toQuestion())
}

// GetAgentQuestion returns the role question with the given id, or ErrNotFound.
func (s *Store) GetAgentQuestion(id int64) (AgentQuestion, error) {
	row := s.db.QueryRow(`SELECT `+agentQuestionColumns+` FROM questions
		WHERE id = ? AND role_id IS NOT NULL`, id)
	return scanAgentQuestion(row)
}

// ListAgentQuestions returns a role's questions, ascending by id. If openOnly
// is true, only questions with status "open" are returned.
func (s *Store) ListAgentQuestions(roleID string, openOnly bool) ([]AgentQuestion, error) {
	query := `SELECT ` + agentQuestionColumns + ` FROM questions WHERE role_id = ?`
	if openOnly {
		query += ` AND status = 'open'`
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, roleID)
	if err != nil {
		return nil, fmt.Errorf("query agent questions: %w", err)
	}
	defer rows.Close()

	var out []AgentQuestion
	for rows.Next() {
		q, err := scanAgentQuestion(rows)
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

// ResolveAgentQuestion transitions a role question from "open" to "resolved"
// with the given resolution ("answered" or "dismissed"). Returns ErrNotFound
// if it doesn't exist and ErrQuestionResolved if it is already resolved.
func (s *Store) ResolveAgentQuestion(id int64, resolution string) error {
	res, err := s.db.Exec(
		`UPDATE questions SET status = 'resolved', resolution = ?, resolved_at = ?
		 WHERE id = ? AND status = 'open'`,
		resolution, time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("resolve agent question: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve agent question rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}

	// No row updated: either it doesn't exist, or it is already resolved.
	if _, err := s.GetAgentQuestion(id); err != nil {
		return err
	}
	return ErrQuestionResolved
}

// ReopenAgentQuestion flips a resolved role question back to open, clearing
// its resolution. Used when the role instance disputes the human's final
// answer by replying into the resolved thread — the disagreement continues in
// the SAME thread (see internal/api/agent_questions.go), exactly as
// orchestrators may do on task questions. Returns ErrNotFound for an unknown
// id and ErrQuestionOpen if the question is not resolved.
func (s *Store) ReopenAgentQuestion(id int64) error {
	res, err := s.db.Exec(
		`UPDATE questions SET status = 'open', resolution = '', resolved_at = NULL
		 WHERE id = ? AND status = 'resolved'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("reopen agent question: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("reopen agent question rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}
	if _, err := s.GetAgentQuestion(id); err != nil {
		return err
	}
	return ErrQuestionOpen
}

// AddAgentQuestionMessage inserts a new message into a role question's thread.
// CreatedAt defaults to now if unset. Returns the assigned id.
func (s *Store) AddAgentQuestionMessage(m AgentQuestionMessage) (int64, error) {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}
	if m.Kind == "" {
		m.Kind = "reply"
	}

	return s.AddQuestionMessage(QuestionMessage{
		QuestionID: m.QuestionID, Author: m.Author, Kind: m.Kind,
		Body: m.Body, CreatedAt: m.CreatedAt,
	})
}

// ListAgentQuestionMessages returns the thread for questionID, ascending by id.
func (s *Store) ListAgentQuestionMessages(questionID int64) ([]AgentQuestionMessage, error) {
	msgs, err := s.ListQuestionMessages(questionID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentQuestionMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, AgentQuestionMessage{
			ID: m.ID, QuestionID: m.QuestionID, Author: m.Author,
			Kind: m.Kind, Body: m.Body, CreatedAt: m.CreatedAt,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// AgentQuestionOrdinal returns q's 1-based position among all questions asked
// on its role, ordered by id (e.g. for "Q3" numbering).
func (s *Store) AgentQuestionOrdinal(q AgentQuestion) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM questions WHERE role_id = ? AND id <= ?`, q.RoleID, q.ID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("compute agent question ordinal: %w", err)
	}
	return n, nil
}

func scanAgentQuestion(row interface{ Scan(...any) error }) (AgentQuestion, error) {
	var q AgentQuestion
	var context, resolution, addressedTo sql.NullString
	var resolvedAt sql.NullInt64

	err := row.Scan(
		&q.ID, &q.RoleID, &q.AskedBy, &q.Body, &context, &q.Status, &resolution,
		&addressedTo, &q.AskedAt, &resolvedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentQuestion{}, ErrNotFound
	}
	if err != nil {
		return AgentQuestion{}, fmt.Errorf("scan agent question: %w", err)
	}

	q.Context = context.String
	q.Resolution = resolution.String
	q.AddressedTo = decodeAddressedTo(addressedTo.String)
	q.ResolvedAt = resolvedAt.Int64

	return q, nil
}
