package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"strings"
	"time"
)

const claudeTransport = "claude-socket-v1"
const claudeOwnsMessage = `(delivery_transport='claude-socket-v1' OR (delivery_transport='' AND EXISTS(SELECT 1 FROM claude_deliveries WHERE message_id=messages.id)))`
const claudeDeliveryColumns = `message_id,transport,wire_id,snapshot_json,claude_uuid,journal_device,journal_inode,journal_path,boundary_offset,envelope_hash,phase,deadline_ns,proof_record,proof_offset,proof_end`

type claudeRow struct {
	Snapshot    claudeinstance.Snapshot
	Record      sql.NullString
	Offset, End sql.NullInt64
}

func envelopeHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Zero is an explicit disabled-age mode. UnixNano outside its representable
// interval is undefined, so require exact positive round-trip for nonzero data.
func claudeDeadlineNS(deadline time.Time) (int64, bool) {
	if deadline.IsZero() {
		return 0, true
	}
	ns := deadline.UnixNano()
	return ns, ns > 0 && time.Unix(0, ns).Equal(deadline)
}
func claudeAgeExpired(deadline, now time.Time) bool {
	return !deadline.IsZero() && !now.Before(deadline)
}
func validClaudeEnvelope(v claudeinstance.Snapshot) bool {
	if v.EnvelopeVersion == 0 {
		return true
	}
	if v.EnvelopeVersion != 1 {
		return false
	}
	if _, err := socketmsg.EnvelopeWithID(v.WireID, "", "", ""); err != nil {
		return false
	}
	open, _, ok := strings.Cut(v.Envelope, ">\n")
	return ok && strings.HasPrefix(open, `<cross-session-message msg-id="`+v.WireID+`"`) && strings.Count(open, "msg-id=") == 1 && strings.HasSuffix(v.Envelope, "\n</cross-session-message>")
}
func validClaudeSnapshot(v claudeinstance.Snapshot) bool {
	t := v.Target
	b := v.Boundary
	_, validDeadline := claudeDeadlineNS(v.Deadline)
	return validClaudeEnvelope(v) && v.MessageID > 0 && v.Transport == claudeTransport && v.WireID != "" && v.Envelope != "" && t.RocketSessionID != "" && t.LaunchGeneration != "" && t.ClaudeSessionID != "" && t.PID > 0 && !t.ProcessStart.IsZero() && t.PaneIdentity != "" && t.PeerProtocol == 1 && t.Kind == "interactive" && t.SocketPath != "" && t.JournalPath != "" && b.Path == t.JournalPath && b.Inode > 0 && b.Offset >= 0 && !v.PreparedAt.IsZero() && validDeadline && (v.Deadline.IsZero() || v.Deadline.After(v.PreparedAt))
}
func scanClaude(row interface{ Scan(...any) error }) (claudeRow, error) {
	var v claudeRow
	var id, device, inode, boundary, deadline int64
	var transport, wire, raw, uuid, path, hash, phase string
	e := row.Scan(&id, &transport, &wire, &raw, &uuid, &device, &inode, &path, &boundary, &hash, &phase, &deadline, &v.Record, &v.Offset, &v.End)
	if errors.Is(e, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal([]byte(raw), &v.Snapshot); e != nil {
		return v, fmt.Errorf("corrupt Claude delivery snapshot")
	}
	s := v.Snapshot
	expectedDeadline, validDeadline := claudeDeadlineNS(s.Deadline)
	if !validClaudeSnapshot(s) || id != s.MessageID || transport != s.Transport || wire != s.WireID || uuid != s.Target.ClaudeSessionID || uint64(device) != s.Boundary.Device || uint64(inode) != s.Boundary.Inode || path != s.Boundary.Path || boundary != s.Boundary.Offset || hash != envelopeHash(s.Envelope) || (!validDeadline || deadline != expectedDeadline) {
		return v, fmt.Errorf("inconsistent Claude delivery snapshot")
	}
	switch phase {
	case "possible_write", "not_sent", "unknown", "accepted", "held", "denied", "expired", "delivered", "unconfirmed":
	default:
		return v, fmt.Errorf("unsupported Claude phase")
	}
	v.Snapshot.Phase = phase
	return v, nil
}
func (s *Store) GetClaudeDelivery(id int64) (claudeinstance.Snapshot, error) {
	v, e := scanClaude(s.db.QueryRow(`SELECT `+claudeDeliveryColumns+` FROM claude_deliveries WHERE message_id=?`, id))
	return v.Snapshot, e
}
func (s *Store) GetClaudeDeliveryByWireID(wire string) (claudeinstance.Snapshot, error) {
	v, e := scanClaude(s.db.QueryRow(`SELECT `+claudeDeliveryColumns+` FROM claude_deliveries WHERE wire_id=?`, wire))
	if e == nil && v.Snapshot.WireID != wire {
		return v.Snapshot, fmt.Errorf("Claude wire mismatch")
	}
	return v.Snapshot, e
}

// claudeHistory validates ALL marked historical rows before using accelerators.
// A missing/corrupt old snapshot cannot disappear behind a UUID/hash filter.
func claudeHistory(tx *sql.Tx) ([]claudeRow, error) {
	return claudeHistoryContext(context.Background(), tx)
}
func claudeHistoryContext(ctx context.Context, tx *sql.Tx) ([]claudeRow, error) {
	var missing int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM messages m LEFT JOIN claude_deliveries c ON c.message_id=m.id WHERE m.delivery_transport=? AND c.message_id IS NULL`, claudeTransport).Scan(&missing)
	if e != nil {
		return nil, e
	}
	if missing > 0 {
		return nil, fmt.Errorf("missing Claude history")
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+claudeDeliveryColumns+` FROM claude_deliveries`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var all []claudeRow
	for rows.Next() {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		v, e := scanClaude(rows)
		if e != nil {
			return nil, e
		}
		all = append(all, v)
	}
	return all, rows.Err()
}
func unresolvedClaude(phase string) bool {
	return phase == "possible_write" || phase == "unknown" || phase == "held" || phase == "accepted" || phase == "unconfirmed"
}
func crossFileUncertain(v claudeinstance.Snapshot, history []claudeRow) bool {
	cross, _ := crossFileUncertainContext(context.Background(), v, history)
	return cross
}
func crossFileUncertainContext(ctx context.Context, v claudeinstance.Snapshot, history []claudeRow) (bool, error) {
	for _, h := range history {
		if e := ctx.Err(); e != nil {
			return false, e
		}
		a := h.Snapshot
		if a.MessageID == v.MessageID || a.Target.ClaudeSessionID != v.Target.ClaudeSessionID || !unresolvedClaude(a.Phase) {
			continue
		}
		// New file history is uncertain for the whole UUID, not merely equal text.
		if a.Boundary.Device != v.Boundary.Device || a.Boundary.Inode != v.Boundary.Inode || a.Boundary.Path != v.Boundary.Path {
			return true, nil
		}
	}
	return false, nil
}
func (s *Store) ClaimClaudeMessage(v claudeinstance.Snapshot) (bool, error) {
	v.Phase = "possible_write"
	v.Target.ProcessStart = v.Target.ProcessStart.UTC()
	v.PreparedAt = v.PreparedAt.UTC()
	v.Deadline = v.Deadline.UTC()
	if v.EnvelopeVersion != 1 || !validClaudeSnapshot(v) || claudeAgeExpired(v.Deadline, time.Now()) {
		return false, fmt.Errorf("invalid Claude prewrite snapshot")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	res, e := tx.Exec(`UPDATE messages SET status='delivering',attempts=attempts+1,delivery_transport=?,inject_at=? WHERE id=? AND status='queued' AND delivery_transport='' AND to_session=? AND NOT EXISTS(SELECT 1 FROM claude_deliveries WHERE message_id=messages.id)`, claudeTransport, v.PreparedAt.UnixMilli(), v.MessageID, v.Target.RocketSessionID)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	if e != nil || n != 1 {
		return false, e
	}
	var binding string
	e = tx.QueryRow(`SELECT identity_json FROM claude_launches WHERE session_id=? AND generation=?`, v.Target.RocketSessionID, v.Target.LaunchGeneration).Scan(&binding)
	if e != nil {
		return false, e
	}
	var target claudeinstance.Identity
	if json.Unmarshal([]byte(binding), &target) != nil || !claudeinstance.SameIdentity(target, v.Target) {
		return false, fmt.Errorf("Claude binding changed before claim")
	}
	history, e := claudeHistoryBeforeClaim(tx, v.MessageID)
	if e != nil {
		return false, e
	}
	if crossFileUncertain(v, history) {
		return false, fmt.Errorf("unresolved Claude journal replacement")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return false, e
	}
	deadlineNS, _ := claudeDeadlineNS(v.Deadline)
	_, e = tx.Exec(`INSERT INTO claude_deliveries(message_id,transport,wire_id,snapshot_json,claude_uuid,journal_device,journal_inode,journal_path,boundary_offset,envelope_hash,phase,deadline_ns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.MessageID, v.Transport, v.WireID, string(raw), v.Target.ClaudeSessionID, v.Boundary.Device, v.Boundary.Inode, v.Boundary.Path, v.Boundary.Offset, envelopeHash(v.Envelope), v.Phase, deadlineNS)
	if e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	return true, nil
}
func claudeHistoryBeforeClaim(tx *sql.Tx, id int64) ([]claudeRow, error) {
	var missing int
	e := tx.QueryRow(`SELECT count(*) FROM messages m LEFT JOIN claude_deliveries c ON c.message_id=m.id WHERE m.delivery_transport=? AND c.message_id IS NULL AND m.id<>?`, claudeTransport, id).Scan(&missing)
	if e != nil {
		return nil, e
	}
	if missing > 0 {
		return nil, fmt.Errorf("missing previous Claude history")
	}
	rows, e := tx.Query(`SELECT ` + claudeDeliveryColumns + ` FROM claude_deliveries`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var all []claudeRow
	for rows.Next() {
		v, e := scanClaude(rows)
		if e != nil {
			return nil, e
		}
		all = append(all, v)
	}
	return all, rows.Err()
}
func allowedClaudePhase(from, to string) bool {
	// Only the trusted queue lifecycle caller requests this terminal phase;
	// receipt validation never maps a wire status to unconfirmed.
	if to == "unconfirmed" {
		return from == "possible_write" || from == "unknown" || from == "held" || from == "accepted"
	}
	switch from {
	case "possible_write":
		return to == "not_sent" || to == "unknown" || to == "held" || to == "accepted" || to == "denied" || to == "expired"
	case "unknown":
		return to == "unknown" || to == "held" || to == "accepted" || to == "denied" || to == "expired"
	case "held":
		return to == "held" || to == "accepted" || to == "denied" || to == "expired"
	case "accepted":
		return to == "accepted"
	}
	return false
}
func (s *Store) SetClaudePhase(id int64, wire string, target claudeinstance.Identity, phase string, now time.Time) (bool, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	res, e := tx.Exec(`UPDATE messages SET status=status,delivery_transport=? WHERE id=? AND `+claudeOwnsMessage+` AND status IN ('delivering','accepted')`, claudeTransport, id)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	if e != nil || n == 0 {
		return false, e
	}
	v, e := scanClaude(tx.QueryRow(`SELECT `+claudeDeliveryColumns+` FROM claude_deliveries WHERE message_id=?`, id))
	if e != nil {
		return false, e
	}
	snap := v.Snapshot
	if snap.WireID != wire || !claudeinstance.SameIdentity(snap.Target, target) || (phase != "unconfirmed" && (claudeAgeExpired(snap.Deadline, now) || claudeAgeExpired(snap.Deadline, time.Now()))) || !allowedClaudePhase(snap.Phase, phase) {
		return false, nil
	}
	status, reason := "accepted", ""
	if phase == "unconfirmed" {
		status = "failed"
		reason = "delivery_unconfirmed"
	}
	if phase == "not_sent" || phase == "denied" || phase == "expired" {
		status = "failed"
		reason = "claude_" + phase
	}
	if _, e = tx.Exec(`UPDATE claude_deliveries SET phase=? WHERE message_id=?`, phase, id); e != nil {
		return false, e
	}
	if _, e = tx.Exec(`UPDATE messages SET status=?,reason=? WHERE id=?`, status, nullIfEmpty(reason), id); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	return true, nil
}
func (s *Store) ExpireClaudeMessage(id int64, now time.Time) (bool, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	res, e := tx.Exec(`UPDATE messages SET status=status,delivery_transport=? WHERE id=? AND `+claudeOwnsMessage+` AND status IN ('queued','delivering','accepted')`, claudeTransport, id)
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	if e != nil || n == 0 {
		return false, e
	}
	v, e := scanClaude(tx.QueryRow(`SELECT `+claudeDeliveryColumns+` FROM claude_deliveries WHERE message_id=?`, id))
	if e == nil && !claudeAgeExpired(v.Snapshot.Deadline, now) && !claudeAgeExpired(v.Snapshot.Deadline, time.Now()) {
		return false, nil
	}
	if _, e = tx.Exec(`UPDATE messages SET status='failed',reason='delivery_unconfirmed' WHERE id=?`, id); e != nil {
		return false, e
	}
	if _, e = tx.Exec(`UPDATE claude_deliveries SET phase='unconfirmed' WHERE message_id=?`, id); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	return true, nil
}
