package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PermissionPromptRow is one Claude Code permission dialog the monitor saw
// on a session's pane (table permission_prompts). ResolvedAt is 0 while the
// dialog is still open.
type PermissionPromptRow struct {
	ID          int64
	SessionID   string
	Title       string
	Context     string
	OptionsJSON string
	AskedAt     int64
	ResolvedAt  int64
	AnswerLabel string
	// AnsweredVia is "chat" (answered through the API) or "terminal"
	// (the dialog went away without one); empty while open.
	AnsweredVia string
	// SentAt is when an API answer's keypress was actually sent (0: never).
	SentAt int64
}

// resolveVia is the answered_via a row gets when it is closed: chat when an
// API answer was recorded on it, terminal otherwise.
const resolveVia = `CASE WHEN answer_label != '' THEN 'chat' ELSE 'terminal' END`

// OpenPermissionPrompt records that sessionID shows the dialog (title,
// context). An open row of the session with the same identity is reused —
// that is what keeps a daemon restart from duplicating the dialog it was
// tracking — and every other open row of the session is closed first, since
// a pane shows one dialog at a time. Returns the row id and whether it was
// reused.
func (s *Store) OpenPermissionPrompt(sessionID, title, context, optionsJSON string, askedAt int64) (int64, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, fmt.Errorf("open permission prompt: begin: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM permission_prompts
		 WHERE session_id = ? AND resolved_at IS NULL AND title = ? AND context = ?
		 ORDER BY id DESC LIMIT 1`,
		sessionID, title, context,
	).Scan(&id)
	reused := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("open permission prompt: find: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE permission_prompts SET resolved_at = ?, answered_via = `+resolveVia+`
		 WHERE session_id = ? AND resolved_at IS NULL AND id != ?`,
		time.Now().Unix(), sessionID, id,
	); err != nil {
		return 0, false, fmt.Errorf("open permission prompt: close others: %w", err)
	}

	if !reused {
		res, err := tx.Exec(
			`INSERT INTO permission_prompts (session_id, title, context, options_json, asked_at) VALUES (?, ?, ?, ?, ?)`,
			sessionID, title, context, optionsJSON, askedAt,
		)
		if err != nil {
			return 0, false, fmt.Errorf("open permission prompt: insert: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, false, fmt.Errorf("open permission prompt: id: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("open permission prompt: commit: %w", err)
	}
	return id, reused, nil
}

// MarkPermissionPromptAnswered records that an API answer with label was
// sent for row id, so closing it later yields answered_via "chat". An empty
// label withdraws the mark (the keypress could not be sent).
func (s *Store) MarkPermissionPromptAnswered(id int64, label string) error {
	if _, err := s.db.Exec(`UPDATE permission_prompts SET answer_label = ? WHERE id = ? AND resolved_at IS NULL`, label, id); err != nil {
		return fmt.Errorf("mark permission prompt answered: %w", err)
	}
	return nil
}

// ResolvePermissionPrompt closes row id at resolvedAt. Closing an already
// closed row is a no-op that keeps the first resolution.
func (s *Store) ResolvePermissionPrompt(id int64, resolvedAt int64) error {
	if _, err := s.db.Exec(
		`UPDATE permission_prompts SET resolved_at = ?, answered_via = `+resolveVia+` WHERE id = ? AND resolved_at IS NULL`,
		resolvedAt, id,
	); err != nil {
		return fmt.Errorf("resolve permission prompt: %w", err)
	}
	return nil
}

// ListResolvedPermissionPrompts returns the closed rows of sessionID in id
// order (which is also the order they were asked and closed in: a session
// has at most one open row at a time).
func (s *Store) ListResolvedPermissionPrompts(sessionID string) ([]PermissionPromptRow, error) {
	rows, err := s.db.Query(
		`SELECT `+permissionPromptColumns+`
		 FROM permission_prompts WHERE session_id = ? AND resolved_at IS NOT NULL ORDER BY id`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list permission prompts: %w", err)
	}
	defer rows.Close()

	var out []PermissionPromptRow
	for rows.Next() {
		r, err := scanPermissionPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const permissionPromptColumns = `id, session_id, title, context, options_json, asked_at, IFNULL(resolved_at, 0), answer_label, answered_via, sent_at`

func scanPermissionPrompt(sc interface{ Scan(...any) error }) (PermissionPromptRow, error) {
	var r PermissionPromptRow
	if err := sc.Scan(&r.ID, &r.SessionID, &r.Title, &r.Context, &r.OptionsJSON, &r.AskedAt, &r.ResolvedAt, &r.AnswerLabel, &r.AnsweredVia, &r.SentAt); err != nil {
		return PermissionPromptRow{}, fmt.Errorf("scan permission prompt: %w", err)
	}
	return r, nil
}

// GetPermissionPrompt returns row id, or ErrNotFound.
func (s *Store) GetPermissionPrompt(id int64) (PermissionPromptRow, error) {
	r, err := scanPermissionPrompt(s.db.QueryRow(`SELECT `+permissionPromptColumns+` FROM permission_prompts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PermissionPromptRow{}, ErrNotFound
	}
	return r, err
}

// MarkPermissionPromptSent records when an API answer's keypress for row id
// was actually sent. The monitor uses it to tell a dialog that is still
// open from an identical one Claude asked right after the answer.
func (s *Store) MarkPermissionPromptSent(id int64, sentAt int64) error {
	if _, err := s.db.Exec(`UPDATE permission_prompts SET sent_at = ? WHERE id = ? AND resolved_at IS NULL`, sentAt, id); err != nil {
		return fmt.Errorf("mark permission prompt sent: %w", err)
	}
	return nil
}

// ResolveOpenPermissionPrompts closes every open row of sessionID — for a
// session that went to a terminal state with a dialog still journalled.
func (s *Store) ResolveOpenPermissionPrompts(sessionID string, resolvedAt int64) error {
	if _, err := s.db.Exec(
		`UPDATE permission_prompts SET resolved_at = ?, answered_via = `+resolveVia+` WHERE session_id = ? AND resolved_at IS NULL`,
		resolvedAt, sessionID,
	); err != nil {
		return fmt.Errorf("resolve open permission prompts: %w", err)
	}
	return nil
}

// DeletePermissionPrompt removes row id — a row the monitor opened but
// could not attach to the session's pending quiz.
func (s *Store) DeletePermissionPrompt(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM permission_prompts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete permission prompt: %w", err)
	}
	return nil
}
