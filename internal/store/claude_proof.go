package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"time"
)

func sameClaudeJournal(a, b claudeinstance.Snapshot) bool {
	return a.Target.ClaudeSessionID == b.Target.ClaudeSessionID && a.Boundary.Device == b.Boundary.Device && a.Boundary.Inode == b.Boundary.Inode
}
func ambiguousClaudeProof(ctx context.Context, v claudeinstance.Snapshot, p claudeinstance.Proof, history []claudeRow) (bool, error) {
	cross, e := crossFileUncertainContext(ctx, v, history)
	if e != nil || cross {
		return cross, e
	}
	for _, h := range history {
		if e := ctx.Err(); e != nil {
			return false, e
		}
		a := h.Snapshot
		if a.MessageID == v.MessageID || !sameClaudeJournal(a, v) || a.Envelope != v.Envelope || a.Boundary.Offset > p.Offset {
			continue
		}
		switch a.Phase {
		case "not_sent", "denied", "expired":
			continue
		case "delivered":
			if h.End.Valid && h.End.Int64 <= v.Boundary.Offset {
				continue
			}
		}
		return true, nil
	}
	return false, nil
}

// ConfirmClaudeMessage owns both the physical record and message CAS in one
// serialized transaction. Full launch identity is not a proof-domain key.
func (s *Store) ConfirmClaudeMessage(id int64, proof claudeinstance.Proof, now time.Time) (bool, error) {
	return s.ConfirmClaudeMessageContext(context.Background(), id, proof, now)
}

// ConfirmClaudeMessageContext shares the caller's scan budget with the atomic
// re-read and mutation; capping cannot extend an inherited deadline.
func (s *Store) ConfirmClaudeMessageContext(ctx context.Context, id int64, proof claudeinstance.Proof, now time.Time) (bool, error) {
	ctx, cancel := claudeProofContext(ctx)
	defer cancel()
	// SQLite's connection busy_timeout otherwise sleeps for 5s inside the
	// driver even after a shorter context expires. Use a private checked-out
	// connection and fail closed on contention, restoring its own setting
	// before returning it to the shared pool. Native/other calls are unchanged.
	conn, e := s.db.Conn(ctx)
	if e != nil {
		return false, e
	}
	defer conn.Close()
	var busy int
	if e = conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); e != nil {
		return false, e
	}
	if _, e = conn.ExecContext(ctx, "PRAGMA busy_timeout=0"); e != nil {
		return false, e
	}
	defer restoreClaudeProofConnection(conn, busy)
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, `UPDATE messages SET status=status,delivery_transport=? WHERE id=? AND `+claudeOwnsMessage+` AND status IN ('delivering','accepted')`, claudeTransport, id)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	if e != nil || n != 1 {
		return false, e
	}
	row, e := scanClaude(tx.QueryRowContext(ctx, `SELECT `+claudeDeliveryColumns+` FROM claude_deliveries WHERE message_id=?`, id))
	if e != nil {
		return false, e
	}
	v := row.Snapshot
	if claudeAgeExpired(v.Deadline, now) || claudeAgeExpired(v.Deadline, time.Now()) || !unresolvedClaude(v.Phase) || v.Phase == "unconfirmed" || proof.RecordID == "" || proof.Offset < v.Boundary.Offset || proof.End <= proof.Offset {
		return false, nil
	}
	history, e := claudeHistoryContext(ctx, tx)
	if e != nil {
		return false, e
	}
	ambiguous, e := ambiguousClaudeProof(ctx, v, proof, history)
	if e != nil || ambiguous {
		return false, e
	}
	// Re-read the exact pinned file under the writer lock so the record chosen
	// by a concurrent scan cannot acquire ownership after history changed.
	actual, found, e := claudeinstance.FindProof(ctx, v)
	if e != nil {
		return false, e
	}
	if !found || actual != proof || claudeAgeExpired(v.Deadline, time.Now()) {
		return false, nil
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	res, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO claude_consumed_records(target_key,device,inode,record_id,message_id) VALUES(?,?,?,?,?)`, v.Target.ClaudeSessionID, v.Boundary.Device, v.Boundary.Inode, proof.RecordID, id)
	if e != nil {
		return false, e
	}
	n, e = res.RowsAffected()
	if e != nil || n != 1 {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE claude_deliveries SET phase='delivered',proof_record=?,proof_offset=?,proof_end=? WHERE message_id=?`, proof.RecordID, proof.Offset, proof.End, id); e != nil {
		return false, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE messages SET status='delivered',delivered_at=?,reason=NULL WHERE id=? AND status IN ('delivering','accepted') AND delivery_transport=?`, now.Unix(), id, claudeTransport); e != nil {
		return false, e
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	return true, nil
}

// Preserve an already shorter caller budget exactly, including its cancellation.
func claudeProofContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= 5*time.Second {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 5*time.Second)
}

type claudeProofConnection interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	Raw(func(any) error) error
}

func restoreClaudeProofConnection(conn claudeProofConnection, busy int) {
	cleanup, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := conn.ExecContext(cleanup, fmt.Sprintf("PRAGMA busy_timeout=%d", busy)); err != nil {
		// database/sql discards a checked-out connection when Raw returns ErrBadConn.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}
