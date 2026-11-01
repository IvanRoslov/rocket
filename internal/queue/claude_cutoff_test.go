package queue

import (
	"context"
	"database/sql"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/store"
	"testing"
	"time"
)

func TestClaudeLegacyUnprovenNeverReplayed(t *testing.T) {
	for _, which := range []string{"old-zero", "old-attempted", "expired", "missing", "corrupt", "post-upgrade"} {
		t.Run(which, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			f := installClaudeSocket(t, h, true)
			db, e := sql.Open("sqlite", h.dbPath)
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			if _, e = db.Exec(`CREATE TABLE IF NOT EXISTS claude_legacy_cutoff(singleton INTEGER PRIMARY KEY CHECK(singleton=1),through_id INTEGER NOT NULL CHECK(through_id>=0)); INSERT OR IGNORE INTO claude_legacy_cutoff VALUES(1,0)`); e != nil {
				t.Fatal(e)
			}
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "held-style feedback"})
			cutoff := id
			if which == "post-upgrade" {
				cutoff = 0
			}
			if _, e := db.Exec(`UPDATE claude_legacy_cutoff SET through_id=?`, cutoff); e != nil {
				t.Fatal(e)
			}
			if which == "old-attempted" {
				h.st.UpdateMessageStatus(id, "queued", 1, 0, "")
			}
			if which == "expired" {
				h.q.cfg.QueueTimeout = time.Nanosecond
			}
			if which == "missing" {
				db.Exec(`DELETE FROM claude_legacy_cutoff`)
			}
			if which == "corrupt" {
				db.Exec(`UPDATE claude_legacy_cutoff SET through_id='bad'`)
			}
			m, _ := h.st.GetMessage(id)
			ctx, cancel := context.WithTimeout(h.ctx, 15*time.Millisecond)
			defer cancel()
			h.q.deliver(ctx, m)
			m, _ = h.st.GetMessage(id)
			want := "legacy_unproven"
			if which == "old-attempted" {
				want = "delivery_unconfirmed"
			}
			if which == "expired" {
				want = "timeout"
			}
			if which == "missing" || which == "corrupt" {
				if m.Status != "queued" || f.sendCount() != 0 || m.Attempts != 0 {
					t.Fatalf("uncertain cutoff %+v", m)
				}
				return
			}
			if which == "post-upgrade" {
				if m.Status != "accepted" || f.sendCount() != 1 {
					t.Fatalf("fresh %+v sends%d", m, f.sendCount())
				}
				return
			}
			if m.Status != "failed" || m.Reason != want || f.sendCount() != 0 || h.rt.callCount() != 0 {
				t.Fatalf("old row %+v sends%d", m, f.sendCount())
			}
		})
	}
}

func TestClaudePostUpgradeFeedbackBeforeLaunch(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	old := installClaudeSocket(t, h, false)
	db, e := sql.Open("sqlite", h.dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`DELETE FROM claude_launches WHERE session_id='recv'`); e != nil {
		t.Fatal(e)
	}
	cutoff, e := h.st.ClaudeLegacyCutoff()
	if e != nil {
		t.Fatal(e)
	}
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback queued before Restore"})
	m, _ := h.st.GetMessage(id)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Millisecond)
	h.q.deliver(ctx, m)
	cancel()
	m, _ = h.st.GetMessage(id)
	if m.Status != "queued" || m.Attempts != 0 || old.sendCount() != 0 {
		t.Fatalf("prelaunch %+v", m)
	}
	fresh := installClaudeSocket(t, h, true)
	m, _ = h.st.GetMessage(id)
	h.q.deliver(h.ctx, m)
	m, _ = h.st.GetMessage(id)
	if m.Status != "accepted" || m.Attempts != 1 || fresh.sendCount() != 1 {
		t.Fatalf("feedback lost after launch %+v", m)
	}
	if n, e := h.st.ClaudeLegacyCutoff(); e != nil || n != cutoff || id <= n {
		t.Fatal(n, e)
	}
}
