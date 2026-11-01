package store

import (
	"path/filepath"
	"testing"
)

func TestClaudeLegacyCutoffMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	st, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if n, e := st.ClaudeLegacyCutoff(); e != nil || n != 0 {
		t.Fatalf("fresh cutoff %d %v", n, e)
	}
	// A pre-upgrade fixture: remove only the unreleased migration's tables/version.
	_, e = st.db.Exec(`DROP TABLE claude_consumed_records; DROP TABLE claude_deliveries; DROP TABLE claude_launches; DROP TABLE claude_legacy_cutoff; DELETE FROM schema_migrations WHERE version=27;`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = st.db.Exec(`INSERT INTO messages(to_session,body,status,created_at) VALUES('old','held','queued',1),('old','old delivered','delivered',1),('other','old outgoing','failed',1)`)
	if e != nil {
		t.Fatal(e)
	}
	var last int64
	st.db.QueryRow(`SELECT MAX(id) FROM messages`).Scan(&last)
	st.Close()
	st, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	n, e := st.ClaudeLegacyCutoff()
	if e != nil || n != last {
		t.Fatalf("upgrade cutoff %d want%d %v", n, last, e)
	}
	id, e := st.AddMessage(Message{ToSession: "old", Body: "post-upgrade feedback"})
	if e != nil || id <= n {
		t.Fatal(id, e)
	}
	// Reopen must not capture the new feedback.
	st.Close()
	st, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	if got, e := st.ClaudeLegacyCutoff(); e != nil || got != n {
		t.Fatalf("recaptured cutoff %d %v", got, e)
	}
}

func TestClaudeLegacyCutoffRejectsCorruption(t *testing.T) {
	for _, sqlText := range []string{`DELETE FROM claude_legacy_cutoff`, `PRAGMA ignore_check_constraints=ON; UPDATE claude_legacy_cutoff SET through_id=-1`, `UPDATE claude_legacy_cutoff SET through_id='oops'`, `UPDATE claude_legacy_cutoff SET through_id=1.5`} {
		t.Run(sqlText, func(t *testing.T) {
			st := openTestStore(t)
			if _, e := st.db.Exec(sqlText); e != nil {
				t.Fatal(e)
			}
			if n, e := st.ClaudeLegacyCutoff(); e == nil {
				t.Fatalf("corrupt cutoff accepted %d", n)
			}
		})
	}
}

func TestClaudeCutoffClaimRechecksAtomically(t *testing.T) {
	for _, which := range []string{"old", "attempted", "missing"} {
		t.Run(which, func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			switch which {
			case "old":
				st.db.Exec(`UPDATE claude_legacy_cutoff SET through_id=?`, id)
			case "attempted":
				st.db.Exec(`UPDATE messages SET attempts=1 WHERE id=?`, id)
			case "missing":
				st.db.Exec(`DELETE FROM claude_legacy_cutoff`)
			}
			if ok, _ := st.ClaimClaudeMessage(v); ok {
				t.Fatal("bypassed first-claim provenance")
			}
			m, _ := st.GetMessage(id)
			var n int
			st.db.QueryRow(`SELECT COUNT(*) FROM claude_deliveries WHERE message_id=?`, id).Scan(&n)
			if m.Status != "queued" || m.DeliveryTransport != "" || n != 0 {
				t.Fatalf("partial claim %+v side%d", m, n)
			}
		})
	}
}
