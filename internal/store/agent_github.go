package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Dedup namespaces for MarkAgentGHSeen. Issue numbers and comment ids are
// both small integers assigned by GitHub in unrelated sequences, so they must
// not share a namespace.
const (
	GHSeenIssue   = "issue_opened"
	GHSeenComment = "issue_comment"
)

// AgentGHWatermark returns the unix timestamp from which the role has been
// watching owner/repo, or 0 when the subscription has never been polled. The
// poller treats 0 as "seed this subscription": record what exists today
// without enqueueing anything, so a fresh subscription does not replay the
// repository's whole backlog into the role inbox.
func (s *Store) AgentGHWatermark(roleID, repo string) (int64, error) {
	var since int64
	err := s.db.QueryRow(
		`SELECT since FROM agent_gh_watermark WHERE role_id = ? AND repo = ?`,
		roleID, repo,
	).Scan(&since)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query gh watermark: %w", err)
	}
	return since, nil
}

// SetAgentGHWatermark stores the watermark for a role's subscription.
func (s *Store) SetAgentGHWatermark(roleID, repo string, since int64) error {
	_, err := s.db.Exec(
		`INSERT INTO agent_gh_watermark (role_id, repo, since, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (role_id, repo) DO UPDATE SET
		        since = excluded.since,
		        updated_at = excluded.updated_at`,
		roleID, repo, since, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert gh watermark: %w", err)
	}
	return nil
}

// MarkAgentGHSeen records that the role has processed the GitHub object
// (issue number or comment id) and reports whether this was the first time.
// A false return means the object was already handled — possibly by an
// earlier run of the daemon — and must not be enqueued again. The insert and
// the check are one statement, so two concurrent ticks cannot both win.
func (s *Store) MarkAgentGHSeen(roleID, repo, kind string, externalID int64) (bool, error) {
	return s.MarkAgentGHSeenAt(roleID, repo, kind, externalID, time.Now().Unix())
}

// MarkAgentGHSeenAt is MarkAgentGHSeen with an explicit created_at, used by
// tests to exercise pruning.
func (s *Store) MarkAgentGHSeenAt(roleID, repo, kind string, externalID, at int64) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO agent_gh_seen (role_id, repo, kind, external_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (role_id, repo, kind, external_id) DO NOTHING`,
		roleID, repo, kind, externalID, at,
	)
	if err != nil {
		return false, fmt.Errorf("insert gh seen: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("gh seen rows affected: %w", err)
	}
	return n > 0, nil
}

// PruneAgentGHSeen drops dedup rows recorded before the given unix timestamp.
// The watermark alone guarantees old objects are not re-fetched, so the seen
// set only has to cover the recent window; pruning keeps it from growing
// without bound on busy repositories.
func (s *Store) PruneAgentGHSeen(before int64) error {
	if _, err := s.db.Exec(`DELETE FROM agent_gh_seen WHERE created_at < ?`, before); err != nil {
		return fmt.Errorf("prune gh seen: %w", err)
	}
	return nil
}
