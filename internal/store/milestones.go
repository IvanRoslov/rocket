package store

import "fmt"

// MilestoneActivity returns, per milestone task id, the unix time of the last
// visible trace left there by the agent holding it (task #1023, spec v2,
// §«Видимость работы» п.2). Milestones nobody has taken, and milestones their
// holder has not touched at all, are absent from the map: the caller decides
// what to measure silence from (the assignment itself), and a zero in the map
// would be indistinguishable from "silent since the epoch".
//
// A trace is anything the agent did in the milestone that a human could read
// later:
//
//   - a journal entry it wrote, except kind=status — take/assign/move write
//     those automatically, and bookkeeping is not work (the same distinction
//     the review gate in internal/api/milestones.go makes);
//   - a doc version it put;
//   - a question it opened on the milestone, or a message it wrote in one.
//
// Everything is attributed by author == tasks.assigned_role, so a doc written
// by the human, or an entry left by a previous holder, never resets the clock
// of the current one.
func (s *Store) MilestoneActivity() (map[int64]int64, error) {
	rows, err := s.db.Query(`
		SELECT t.id, MAX(a.at)
		FROM tasks t
		JOIN (
			SELECT task_id, author AS who, created_at AS at FROM task_log WHERE kind != 'status'
			UNION ALL
			SELECT task_id, author, created_at FROM task_docs
			UNION ALL
			SELECT task_id, asked_by, asked_at FROM questions WHERE task_id IS NOT NULL
			UNION ALL
			SELECT q.task_id, m.author, m.created_at
			FROM question_messages m JOIN questions q ON q.id = m.question_id
			WHERE q.task_id IS NOT NULL
		) a ON a.task_id = t.id AND a.who = t.assigned_role
		WHERE t.milestone = 1 AND COALESCE(t.assigned_role, '') != ''
		GROUP BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("query milestone activity: %w", err)
	}
	defer rows.Close()

	out := make(map[int64]int64)
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan milestone activity: %w", err)
		}
		out[id] = at
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
