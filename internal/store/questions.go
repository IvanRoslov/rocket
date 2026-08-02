package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ParticipantHuman is the participant id of the human user. The other two
// forms a participant id takes are a persistent agent's id ("cto") and a
// session id ("reply-answer-orch").
const ParticipantHuman = "human"

// IsHuman reports whether a stored author or participant id denotes the human.
// Rows written before migration 0009 recorded the human as an empty string or
// NULL, so both forms are accepted; everything the store writes now is
// canonical.
func IsHuman(id string) bool {
	return id == "" || id == ParticipantHuman
}

// Question is a Q&A thread's originating question. A thread is bound to a
// task, to a persistent agent's role, or to neither.
type Question struct {
	ID         int64
	TaskID     int64  // 0 = not bound to a task
	RoleID     string // "" = not bound to a role
	AskedBy    string // session id of the asker; "" = the human
	Body       string
	Context    string // optional markdown context
	Status     string // open|resolved
	Resolution string // answered|dismissed (set once resolved)
	AskedAt    int64
	ResolvedAt int64 // 0 = not resolved
}

// QuestionMessage represents a single entry in a question's thread: either a
// reply (thread stays open) or the resolving answer (thread becomes resolved).
type QuestionMessage struct {
	ID         int64
	QuestionID int64
	Author     string // participant id; ParticipantHuman for the human
	Kind       string // reply|answer
	Body       string
	// AddressedTo narrows who is expected to respond. Empty means every
	// participant except the author.
	AddressedTo []string
	CreatedAt   int64
}

// questionColumns is the column list every Question scan relies on; it must
// stay in sync with scanQuestion.
const questionColumns = `id, task_id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at`

// encodeAddressedTo renders a recipient list as the CSV stored in
// question_messages.addressed_to. An empty list stores as "".
func encodeAddressedTo(ids []string) string {
	return strings.Join(ids, ",")
}

// decodeAddressedTo parses question_messages.addressed_to back into a list.
// "" decodes to nil, not to a one-element slice holding "".
func decodeAddressedTo(csv string) []string {
	if csv == "" {
		return nil
	}
	return strings.Split(csv, ",")
}

// canonicalParticipant maps the legacy empty author to the canonical human id
// so participant-id columns never hold "".
func canonicalParticipant(id string) string {
	if id == "" {
		return ParticipantHuman
	}
	return id
}

// AddQuestion inserts a new question with status "open" and AskedAt
// defaulted to now (unless already set). Returns the assigned id.
func (s *Store) AddQuestion(q Question) (int64, error) {
	if q.AskedAt == 0 {
		q.AskedAt = time.Now().Unix()
	}
	if q.Status == "" {
		q.Status = "open"
	}

	res, err := s.db.Exec(
		`INSERT INTO questions (task_id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfZero(q.TaskID), nullIfEmpty(q.RoleID), q.AskedBy, q.Body, nullIfEmpty(q.Context),
		q.Status, nullIfEmpty(q.Resolution), q.AskedAt, nullIfZero(q.ResolvedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert question: %w", err)
	}
	return res.LastInsertId()
}

// GetQuestion returns the question with the given id, or ErrNotFound.
func (s *Store) GetQuestion(id int64) (Question, error) {
	row := s.db.QueryRow(`SELECT `+questionColumns+` FROM questions WHERE id = ?`, id)
	return scanQuestion(row)
}

// ListQuestions returns questions for taskID, ascending by id. If openOnly
// is true, only questions with status "open" are returned.
func (s *Store) ListQuestions(taskID int64, openOnly bool) ([]Question, error) {
	query := `SELECT ` + questionColumns + ` FROM questions WHERE task_id = ?`
	if openOnly {
		query += ` AND status = 'open'`
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("query questions: %w", err)
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

// ResolveQuestion transitions question id from status "open" to "resolved"
// with the given resolution ("answered" or "dismissed"), setting resolved_at
// to now. Returns ErrNotFound if the question doesn't exist, and
// ErrQuestionResolved if it is already resolved.
func (s *Store) ResolveQuestion(id int64, resolution string) error {
	res, err := s.db.Exec(
		`UPDATE questions SET status = 'resolved', resolution = ?, resolved_at = ?
		 WHERE id = ? AND status = 'open'`,
		resolution, time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("resolve question: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve question rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}

	// No row updated: either the question doesn't exist, or it exists but
	// is already resolved. Distinguish the two for a clearer error.
	if _, err := s.GetQuestion(id); err != nil {
		return err
	}
	return ErrQuestionResolved
}

// AddQuestionMessage inserts a new message into a question's thread.
// CreatedAt defaults to now if unset. Returns the assigned id.
func (s *Store) AddQuestionMessage(m QuestionMessage) (int64, error) {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}
	if m.Kind == "" {
		m.Kind = "reply"
	}

	res, err := s.db.Exec(
		`INSERT INTO question_messages (question_id, author, kind, body, addressed_to, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.QuestionID, canonicalParticipant(m.Author), m.Kind, m.Body,
		encodeAddressedTo(m.AddressedTo), m.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert question message: %w", err)
	}
	return res.LastInsertId()
}

// ListQuestionMessages returns the thread for questionID, ascending by id.
func (s *Store) ListQuestionMessages(questionID int64) ([]QuestionMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, question_id, author, kind, body, addressed_to, created_at
		 FROM question_messages WHERE question_id = ? ORDER BY id`, questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query question messages: %w", err)
	}
	defer rows.Close()

	var out []QuestionMessage
	for rows.Next() {
		var m QuestionMessage
		var author, addressedTo sql.NullString
		if err := rows.Scan(&m.ID, &m.QuestionID, &author, &m.Kind, &m.Body, &addressedTo, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan question message: %w", err)
		}
		m.Author = canonicalParticipant(author.String)
		m.AddressedTo = decodeAddressedTo(addressedTo.String)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// QuestionOrdinal returns q's 1-based position among all questions asked on
// its task, ordered by id (e.g. for "Q3" numbering).
func (s *Store) QuestionOrdinal(q Question) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM questions WHERE task_id = ? AND id <= ?`, q.TaskID, q.ID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("compute question ordinal: %w", err)
	}
	return n, nil
}

// QuestionCounts summarizes a task's open questions for board/list views.
type QuestionCounts struct {
	Open         int
	AwaitingUser int
}

// OpenQuestionCounts returns, per task with at least one open question, how
// many questions are open and how many of those await the human. "Awaiting
// the human" mirrors the whoseTurn derivation in internal/api/questions.go:
// with no thread messages the question itself counts as the last entry (so
// an orchestrator-opened question awaits the human, a user-opened one
// doesn't); otherwise the last message's author decides (orchestrator
// author -> human's turn). Computed in one query so list/board handlers can
// annotate every task without an N+1.
func (s *Store) OpenQuestionCounts() (map[int64]QuestionCounts, error) {
	rows, err := s.db.Query(`
		SELECT task_id, COUNT(*), SUM(turn_user) FROM (
			SELECT q.task_id AS task_id,
				CASE
					WHEN m.id IS NULL THEN (CASE WHEN q.asked_by != '' THEN 1 ELSE 0 END)
					WHEN m.author IS NOT NULL AND m.author != '' THEN 1
					ELSE 0
				END AS turn_user
			FROM questions q
			LEFT JOIN question_messages m
				ON m.question_id = q.id
				AND m.id = (SELECT MAX(id) FROM question_messages WHERE question_id = q.id)
			WHERE q.status = 'open'
		) GROUP BY task_id`)
	if err != nil {
		return nil, fmt.Errorf("query open question counts: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]QuestionCounts)
	for rows.Next() {
		var taskID int64
		var c QuestionCounts
		if err := rows.Scan(&taskID, &c.Open, &c.AwaitingUser); err != nil {
			return nil, fmt.Errorf("scan open question counts: %w", err)
		}
		out[taskID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListAllOpenQuestions returns every open question across all tasks,
// ascending by id — the backing query for the dashboard's global
// Questions page.
func (s *Store) ListAllOpenQuestions() ([]Question, error) {
	rows, err := s.db.Query(
		`SELECT `+questionColumns+` FROM questions
		 WHERE status = 'open' AND task_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query all open questions: %w", err)
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

func scanQuestion(row interface{ Scan(...any) error }) (Question, error) {
	var q Question
	var roleID, context, resolution sql.NullString
	var taskID, resolvedAt sql.NullInt64

	err := row.Scan(
		&q.ID, &taskID, &roleID, &q.AskedBy, &q.Body, &context, &q.Status, &resolution,
		&q.AskedAt, &resolvedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrNotFound
	}
	if err != nil {
		return Question{}, fmt.Errorf("scan question: %w", err)
	}

	q.TaskID = taskID.Int64
	q.RoleID = roleID.String
	q.Context = context.String
	q.Resolution = resolution.String
	q.ResolvedAt = resolvedAt.Int64

	return q, nil
}
