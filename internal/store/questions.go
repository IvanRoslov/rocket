package store

import (
	"database/sql"
	"encoding/json"
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
	// AddressedTo narrows who is expected to respond before the thread has
	// any messages. Empty means every participant except the asker. It
	// mirrors QuestionMessage.AddressedTo, so the turn is derived the same
	// way whether or not anyone has replied yet.
	AddressedTo []string
	// Type is QuestionTypeDecision (a thread that waits for a decision and
	// lights badges) or QuestionTypeFYI (a status note: created already
	// resolved, waiting on nobody). Empty on input means decision.
	Type string
	// Options are the answer choices a client may render as buttons. Stored
	// as a JSON array of strings; nil means the thread has none.
	Options    []string
	AskedAt    int64
	ResolvedAt int64 // 0 = not resolved
}

// Thread types. A decision thread waits for somebody's turn; an fyi thread is
// a status note that is born resolved (resolution QuestionResolutionFYI) and
// never lights a badge — a reply into it reopens it as a decision thread.
const (
	QuestionTypeDecision = "decision"
	QuestionTypeFYI      = "fyi"

	QuestionResolutionFYI = "fyi"
)

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
const questionColumns = `id, task_id, role_id, asked_by, body, context, status, resolution, addressed_to, type, options, asked_at, resolved_at`

// encodeOptions renders answer choices as the JSON stored in
// questions.options. An empty list stores as "" rather than "[]", so a thread
// without options is spelled one way only.
func encodeOptions(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	b, err := json.Marshal(opts)
	if err != nil {
		// []string always marshals; this cannot happen.
		return ""
	}
	return string(b)
}

// decodeOptions parses questions.options back into a list. "" decodes to nil,
// and so does anything unparseable: a corrupt value must not fail every read
// of an otherwise healthy thread.
func decodeOptions(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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
	if q.Type == "" {
		q.Type = QuestionTypeDecision
	}

	res, err := s.db.Exec(
		`INSERT INTO questions (task_id, role_id, asked_by, body, context, status, resolution, addressed_to, type, options, asked_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullIfZero(q.TaskID), nullIfEmpty(q.RoleID), q.AskedBy, q.Body, nullIfEmpty(q.Context),
		q.Status, nullIfEmpty(q.Resolution), encodeAddressedTo(q.AddressedTo),
		q.Type, encodeOptions(q.Options),
		q.AskedAt, nullIfZero(q.ResolvedAt),
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

// QuestionCounts summarizes a subject's open threads for board/list views.
// AwaitingUser means "the human is in waiting_on"; it is derived in
// internal/api from ListOpenThreads, never in SQL, so it cannot drift from the
// waiting_on a thread response shows.
type QuestionCounts struct {
	Open         int
	AwaitingUser int
}

// ListAllOpenQuestions returns every open question across all tasks,
// ascending by id — the backing query for the dashboard's global
// Questions page.
func (s *Store) ListAllOpenQuestions() ([]Question, error) {
	rows, err := s.db.Query(
		`SELECT ` + questionColumns + ` FROM questions
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
	var roleID, context, resolution, addressedTo sql.NullString
	var qType, options sql.NullString
	var taskID, resolvedAt sql.NullInt64

	err := row.Scan(
		&q.ID, &taskID, &roleID, &q.AskedBy, &q.Body, &context, &q.Status, &resolution,
		&addressedTo, &qType, &options, &q.AskedAt, &resolvedAt,
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
	q.AddressedTo = decodeAddressedTo(addressedTo.String)
	q.Type = qType.String
	if q.Type == "" {
		q.Type = QuestionTypeDecision
	}
	q.Options = decodeOptions(options.String)
	q.ResolvedAt = resolvedAt.Int64

	return q, nil
}
