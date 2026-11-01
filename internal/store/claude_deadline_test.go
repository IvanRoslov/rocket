package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeDisabledDeadlineRoundTripAndProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rocket.db")
	st, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	v, id := claudeStoreFixture(t, st)
	v.Deadline = time.Time{}
	if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
		t.Fatalf("disabled claim %v %v", ok, e)
	}
	var ns int64
	if e := st.db.QueryRow(`SELECT deadline_ns FROM claude_deliveries WHERE message_id=?`, id).Scan(&ns); e != nil || ns != 0 {
		t.Fatalf("disabled SQL encoding=%d error=%v", ns, e)
	}
	if e := st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	st.ResetDelivering()
	m, _ := st.GetMessage(id)
	if m.Status != "accepted" {
		t.Fatalf("restart requeued disabled side effect: %+v", m)
	}
	saved, e := st.GetClaudeDelivery(id)
	if e != nil || !saved.Deadline.IsZero() {
		t.Fatalf("disabled roundtrip=%s error=%v", saved.Deadline, e)
	}
	if ok, e := st.ExpireClaudeMessage(id, time.Now().Add(365*24*time.Hour)); ok || e != nil {
		t.Fatalf("disabled age expired %v %v", ok, e)
	}
	p := appendClaudeIncoming(t, v, "late-exact")
	if ok, e := st.ConfirmClaudeMessage(id, p, time.Now().Add(365*24*time.Hour)); !ok || e != nil {
		t.Fatalf("disabled late proof %v %v", ok, e)
	}
}
func TestClaudeDeadlineEncodingMismatchAndInvalidNonzeroFailClosed(t *testing.T) {
	for _, which := range []string{"nonzero JSON SQL0", "zero JSON positive SQL", "before prepared", "unrepresentable future", "negative encoding"} {
		t.Run(which, func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			switch which {
			case "before prepared":
				v.Deadline = v.PreparedAt.Add(-time.Second)
			case "unrepresentable future":
				v.Deadline = time.Date(2600, 1, 1, 0, 0, 0, 0, time.UTC)
			case "negative encoding":
				v.PreparedAt = time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC)
				v.Deadline = v.PreparedAt.Add(time.Hour)
			}
			if which == "nonzero JSON SQL0" || which == "zero JSON positive SQL" {
				if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
					t.Fatal(e)
				}
				if which == "nonzero JSON SQL0" {
					st.db.Exec(`UPDATE claude_deliveries SET deadline_ns=0 WHERE message_id=?`, id)
				} else {
					v.Deadline = time.Time{}
					raw, _ := json.Marshal(v)
					st.db.Exec(`UPDATE claude_deliveries SET snapshot_json=? WHERE message_id=?`, string(raw), id)
				}
				if _, e := st.GetClaudeDelivery(id); e == nil {
					t.Fatal("JSON/SQL mode disagreement accepted")
				}
			} else if ok, _ := st.ClaimClaudeMessage(v); ok {
				t.Fatal("invalid nonzero deadline accepted")
			}
		})
	}
}
func TestClaudeLifecycleUnconfirmedCASPreservesTerminalAndOwnership(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		st := openTestStore(t)
		v, id := claudeStoreFixture(t, st)
		if disabled {
			v.Deadline = time.Time{}
		}
		if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
			t.Fatalf("claim %v %v", ok, e)
		}
		wrong := v.Target
		wrong.LaunchGeneration = "foreign"
		if ok, _ := st.SetClaudePhase(id, v.WireID, wrong, "unconfirmed", time.Now()); ok {
			t.Fatal("foreign lifecycle target settled")
		}
		if ok, _ := st.SetClaudePhase(id, "foreign", v.Target, "unconfirmed", time.Now()); ok {
			t.Fatal("foreign wire settled")
		}
		if ok, e := st.SetClaudePhase(id, v.WireID, v.Target, "unconfirmed", time.Now()); !ok || e != nil {
			t.Fatalf("trusted lifecycle close %v %v", ok, e)
		}
		m, _ := st.GetMessage(id)
		side, _ := st.GetClaudeDelivery(id)
		if m.Status != "failed" || m.Reason != "delivery_unconfirmed" || side.Phase != "unconfirmed" {
			t.Fatalf("non-atomic lifecycle closure %+v phase=%s", m, side.Phase)
		}
		p := appendClaudeIncoming(t, v, "too-late")
		if ok, _ := st.ConfirmClaudeMessage(id, p, time.Now()); ok {
			t.Fatal("terminal reopened by proof")
		}
		if ok, _ := st.SetClaudePhase(id, v.WireID, v.Target, "accepted", time.Now()); ok {
			t.Fatal("terminal reopened by receipt")
		}
	}
}
