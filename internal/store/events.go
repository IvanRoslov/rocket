package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Event represents an entry in the append-only event log.
type Event struct {
	ID        int64
	TS        int64
	Type      string
	SessionID string
	Data      map[string]any
}

// AppendEvent inserts a new event and returns its assigned id.
func (s *Store) AppendEvent(e Event) (int64, error) {
	if e.TS == 0 {
		e.TS = time.Now().Unix()
	}

	dataJSON := "{}"
	if e.Data != nil {
		b, err := json.Marshal(e.Data)
		if err != nil {
			return 0, fmt.Errorf("marshal data: %w", err)
		}
		dataJSON = string(b)
	}

	res, err := s.db.Exec(
		`INSERT INTO events (ts, type, session_id, data) VALUES (?, ?, ?, ?)`,
		e.TS, e.Type, nullIfEmpty(e.SessionID), dataJSON,
	)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	return res.LastInsertId()
}

// ListEvents returns events with id > sinceID, optionally filtered by
// sessionID (when non-empty), ordered by id ascending, up to limit rows.
// A limit <= 0 means no limit.
func (s *Store) ListEvents(sinceID int64, limit int, sessionID string) ([]Event, error) {
	query := `SELECT id, ts, type, session_id, data FROM events WHERE id > ?`
	args := []any{sinceID}

	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY id`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var sessID sql.NullString
		var dataJSON string

		if err := rows.Scan(&e.ID, &e.TS, &e.Type, &sessID, &dataJSON); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.SessionID = sessID.String

		if err := json.Unmarshal([]byte(dataJSON), &e.Data); err != nil {
			return nil, fmt.Errorf("unmarshal data: %w", err)
		}

		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
