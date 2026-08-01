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
	AskedAt    int64
	ResolvedAt int64 // 0 = not resolved
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

const agentQuestionColumns = `id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at`

// AddAgentQuestion inserts a new role question with status "open" and AskedAt
// defaulted to now (unless already set). Returns the assigned id.
func (s *Store) AddAgentQuestion(q AgentQuestion) (int64, error) {
	if q.AskedAt == 0 {
		q.AskedAt = time.Now().Unix()
	}
	if q.Status == "" {
		q.Status = "open"
	}

	res, err := s.db.Exec(
		`INSERT INTO agent_questions (role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		q.RoleID, q.AskedBy, q.Body, nullIfEmpty(q.Context), q.Status, nullIfEmpty(q.Resolution),
		q.AskedAt, nullIfZero(q.ResolvedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert agent question: %w", err)
	}
	return res.LastInsertId()
}

// GetAgentQuestion returns the role question with the given id, or ErrNotFound.
func (s *Store) GetAgentQuestion(id int64) (AgentQuestion, error) {
	row := s.db.QueryRow(`SELECT `+agentQuestionColumns+` FROM agent_questions WHERE id = ?`, id)
	return scanAgentQuestion(row)
}

// ListAgentQuestions returns a role's questions, ascending by id. If openOnly
// is true, only questions with status "open" are returned.
func (s *Store) ListAgentQuestions(roleID string, openOnly bool) ([]AgentQuestion, error) {
	query := `SELECT ` + agentQuestionColumns + ` FROM agent_questions WHERE role_id = ?`
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
		`UPDATE agent_questions SET status = 'resolved', resolution = ?, resolved_at = ?
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
		`UPDATE agent_questions SET status = 'open', resolution = '', resolved_at = NULL
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

	res, err := s.db.Exec(
		`INSERT INTO agent_question_messages (question_id, author, kind, body, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		m.QuestionID, nullIfEmpty(m.Author), m.Kind, m.Body, m.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert agent question message: %w", err)
	}
	return res.LastInsertId()
}

// ListAgentQuestionMessages returns the thread for questionID, ascending by id.
func (s *Store) ListAgentQuestionMessages(questionID int64) ([]AgentQuestionMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, question_id, author, kind, body, created_at
		 FROM agent_question_messages WHERE question_id = ? ORDER BY id`, questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query agent question messages: %w", err)
	}
	defer rows.Close()

	var out []AgentQuestionMessage
	for rows.Next() {
		var m AgentQuestionMessage
		var author sql.NullString
		if err := rows.Scan(&m.ID, &m.QuestionID, &author, &m.Kind, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent question message: %w", err)
		}
		m.Author = author.String
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AgentQuestionOrdinal returns q's 1-based position among all questions asked
// on its role, ordered by id (e.g. for "Q3" numbering).
func (s *Store) AgentQuestionOrdinal(q AgentQuestion) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM agent_questions WHERE role_id = ? AND id <= ?`, q.RoleID, q.ID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("compute agent question ordinal: %w", err)
	}
	return n, nil
}

// OpenAgentQuestionCounts returns, per role with at least one open question,
// how many questions are open and how many of those await the human. Mirrors
// OpenQuestionCounts: with no thread messages the question itself counts as
// the last entry (so a role-opened question awaits the human, a human-opened
// one doesn't); otherwise the last message's author decides (role author ->
// human's turn). Computed in one query so the agents list handler can annotate
// every role without an N+1.
func (s *Store) OpenAgentQuestionCounts() (map[string]QuestionCounts, error) {
	rows, err := s.db.Query(`
		SELECT role_id, COUNT(*), SUM(turn_user) FROM (
			SELECT q.role_id AS role_id,
				CASE
					WHEN m.id IS NULL THEN (CASE WHEN q.asked_by != '' THEN 1 ELSE 0 END)
					WHEN m.author IS NOT NULL AND m.author != '' THEN 1
					ELSE 0
				END AS turn_user
			FROM agent_questions q
			LEFT JOIN agent_question_messages m
				ON m.question_id = q.id
				AND m.id = (SELECT MAX(id) FROM agent_question_messages WHERE question_id = q.id)
			WHERE q.status = 'open'
		) GROUP BY role_id`)
	if err != nil {
		return nil, fmt.Errorf("query open agent question counts: %w", err)
	}
	defer rows.Close()

	out := make(map[string]QuestionCounts)
	for rows.Next() {
		var roleID string
		var c QuestionCounts
		if err := rows.Scan(&roleID, &c.Open, &c.AwaitingUser); err != nil {
			return nil, fmt.Errorf("scan open agent question counts: %w", err)
		}
		out[roleID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanAgentQuestion(row interface{ Scan(...any) error }) (AgentQuestion, error) {
	var q AgentQuestion
	var context, resolution sql.NullString
	var resolvedAt sql.NullInt64

	err := row.Scan(
		&q.ID, &q.RoleID, &q.AskedBy, &q.Body, &context, &q.Status, &resolution, &q.AskedAt, &resolvedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentQuestion{}, ErrNotFound
	}
	if err != nil {
		return AgentQuestion{}, fmt.Errorf("scan agent question: %w", err)
	}

	q.Context = context.String
	q.Resolution = resolution.String
	q.ResolvedAt = resolvedAt.Int64

	return q, nil
}
