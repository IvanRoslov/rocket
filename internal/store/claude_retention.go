package store

import "database/sql"

// protectClaudeHistory records only required message IDs in a transaction-local
// temporary set. No history copy, archive, timer-based GC or public API exists.
func protectClaudeHistory(tx *sql.Tx) error {
	if _, e := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS rocket_claude_retention(id INTEGER PRIMARY KEY)`); e != nil {
		return e
	}
	if _, e := tx.Exec(`DELETE FROM rocket_claude_retention`); e != nil {
		return e
	}
	history, e := claudeHistory(tx)
	if e != nil {
		// Damaged ownership cannot be safely excluded through an indexed filter.
		// Keep Claude evidence; unrelated native/legacy purge continues normally.
		_, e = tx.Exec(`INSERT OR IGNORE INTO rocket_claude_retention SELECT id FROM messages WHERE delivery_transport='claude-socket-v1' OR EXISTS(SELECT 1 FROM claude_deliveries WHERE message_id=messages.id)`)
		return e
	}
	type physicalEnvelope struct {
		uuid          string
		device, inode uint64
		envelope      string
	}
	key := func(a claudeRow) physicalEnvelope {
		v := a.Snapshot
		return physicalEnvelope{v.Target.ClaudeSessionID, v.Boundary.Device, v.Boundary.Inode, v.Envelope}
	}
	// Exact string keys preserve byte equality; no hash-only ownership. Build
	// the earliest unresolved boundary once rather than a quadratic join.
	earliest := map[physicalEnvelope]int64{}
	protected := map[int64]bool{}
	for _, a := range history {
		if !unresolvedClaude(a.Snapshot.Phase) {
			continue
		}
		protected[a.Snapshot.MessageID] = true
		k := key(a)
		offset := a.Snapshot.Boundary.Offset
		if previous, found := earliest[k]; !found || offset < previous {
			earliest[k] = offset
		}
	}
	for _, a := range history {
		if a.Snapshot.Phase != "delivered" {
			continue
		}
		if !a.Record.Valid || !a.Offset.Valid || !a.End.Valid {
			protected[a.Snapshot.MessageID] = true
			continue
		}
		if offset, found := earliest[key(a)]; found && a.End.Int64 > offset {
			protected[a.Snapshot.MessageID] = true
		}
	}
	for id := range protected {
		if _, e := tx.Exec(`INSERT INTO rocket_claude_retention(id) VALUES(?)`, id); e != nil {
			return e
		}
	}
	return nil
}
