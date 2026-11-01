package store

import (
	"context"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeNewClaimRequiresEnvelopeVersion1(t *testing.T) {
	for _, c := range []struct {
		name    string
		version int
		tag     string
	}{
		{"legacy", 0, `<cross-session-message>`},
		{"unknown", 2, `<cross-session-message msg-id="08edca10-0082-4bd4-b91a-a687a2690ba3">`},
		{"missing", 1, `<cross-session-message>`},
		{"duplicate", 1, `<cross-session-message msg-id="08edca10-0082-4bd4-b91a-a687a2690ba3" msg-id="08edca10-0082-4bd4-b91a-a687a2690ba3">`},
		{"mismatch", 1, `<cross-session-message msg-id="18edca10-0082-4bd4-b91a-a687a2690ba3">`},
		{"malformed", 1, `<cross-session-message msg-id="bad">`},
	} {
		t.Run(c.name, func(t *testing.T) {
			st := openTestStore(t)
			v, id := claudeStoreFixture(t, st)
			v.Envelope = c.tag + "\nfeedback\n</cross-session-message>"
			raw, _ := json.Marshal(v)
			var m map[string]any
			json.Unmarshal(raw, &m)
			m["EnvelopeVersion"] = c.version
			raw, _ = json.Marshal(m)
			json.Unmarshal(raw, &v)
			if ok, _ := st.ClaimClaudeMessage(v); ok {
				t.Fatal("invalid new claim accepted")
			}
			msg, _ := st.GetMessage(id)
			if msg.Status != "queued" || msg.DeliveryTransport != "" {
				t.Fatalf("partial claim %+v", msg)
			}
		})
	}
}

func TestClaudeEqualBodyDistinctWireProof(t *testing.T) {
	st := openTestStore(t)
	a, _ := claudeStoreFixture(t, st)
	// Prepare two independently identified envelopes, not two copies of one body.
	raw, _ := json.Marshal(a)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	fields["EnvelopeVersion"] = 1
	raw, _ = json.Marshal(fields)
	json.Unmarshal(raw, &a)
	a.Envelope = "<cross-session-message msg-id=\"" + a.WireID + "\">\nfeedback\n</cross-session-message>"
	if ok, e := st.ClaimClaudeMessage(a); !ok || e != nil {
		t.Fatalf("claim A %v %v", ok, e)
	}
	b := nextClaudeSnapshot(t, st, a, "g2")
	b.Envelope = strings.Replace(a.Envelope, a.WireID, b.WireID, 1)
	if ok, e := st.ClaimClaudeMessage(b); !ok || e != nil {
		t.Fatalf("claim B %v %v", ok, e)
	}
	p := appendClaudeIncoming(t, a, "late-A")
	if ok, e := st.ConfirmClaudeMessage(b.MessageID, p, b.PreparedAt); ok || e != nil {
		t.Fatalf("A proved B %v %v", ok, e)
	}
}

func TestClaudeCrossUUIDAndSessionT1Matrix(t *testing.T) {
	for _, which := range []string{"same-root", "restored-root", "different-session", "legacy-v0"} {
		t.Run(which, func(t *testing.T) {
			st := openTestStore(t)
			a, _ := claudeStoreFixture(t, st)
			if which == "legacy-v0" {
				a.EnvelopeVersion = 0
				a.Envelope = "<cross-session-message>\nfeedback\n</cross-session-message>"
				claimLegacyClaudeFixture(t, st, a)
			} else if ok, e := st.ClaimClaudeMessage(a); !ok || e != nil {
				t.Fatal(ok, e)
			}
			b := a
			b.EnvelopeVersion = 1
			b.Target.ClaudeSessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
			b.Target.JournalPath = filepath.Join(filepath.Dir(a.Target.JournalPath), b.Target.ClaudeSessionID+".jsonl")
			if e := os.WriteFile(b.Target.JournalPath, nil, 0600); e != nil {
				t.Fatal(e)
			}
			if which == "different-session" {
				b.Target.RocketSessionID = "other"
				b.Target.LaunchGeneration = "other-generation"
				b.Target.PID++
				st.AddSession(Session{ID: "other", Kind: "worker", Agent: "deepseek", State: "running", TmuxName: "other"})
				l, _, _ := st.GetClaudeLaunch("s")
				l.RocketSessionID = "other"
				l.Generation = b.Target.LaunchGeneration
				l.StartedAt = time.Now()
				st.PutClaudeLaunch(l)
				if ok, e := st.BindClaudeIdentity(b.Target); !ok || e != nil {
					t.Fatal(ok, e)
				}
			} else if which == "restored-root" {
				l, _, _ := st.GetClaudeLaunch("s")
				l.Generation = "restored"
				l.StartedAt = time.Now()
				st.PutClaudeLaunch(l)
				b.Target.LaunchGeneration = l.Generation
				b.Target.PID++
				b.Target.ProcessStart = time.Now()
				if ok, e := st.BindClaudeIdentity(b.Target); !ok || e != nil {
					t.Fatal(ok, e)
				}
			} else {
				l, _, at, _ := st.GetClaudeBinding(context.Background(), "s")
				if ok, e := st.AdvanceClaudeBinding(context.Background(), a.Target, b.Target, l.BindingRevision, at, time.Now().UnixNano()); !ok || e != nil {
					t.Fatal(ok, e)
				}
			}
			l, _, barrier, _ := st.GetClaudeBinding(context.Background(), b.Target.RocketSessionID)
			b.BindingRevision = l.BindingRevision
			if ok, e := st.ApplyClaudeActivityForBinding(context.Background(), b.Target, l.BindingRevision, barrier, "ready", time.Now().Unix()); !ok || e != nil {
				t.Fatal(ok, e)
			}
			b.MessageID, _ = st.AddMessage(Message{ToSession: b.Target.RocketSessionID, Body: "feedback"})
			b.WireID, _ = socketmsg.NewMsgID()
			b.Envelope, _ = socketmsg.EnvelopeWithID(b.WireID, "", "", "feedback")
			b.Boundary, _ = claudeinstance.PinJournal(b.Target)
			b.PreparedAt = time.Now()
			b.Deadline = b.PreparedAt.Add(time.Hour)
			if ok, e := st.ClaimClaudeMessage(b); !ok || e != nil {
				t.Fatal(ok, e)
			}
			// M1 drains into B after M2's boundary; UUID metadata is B, text still M1.
			late := b
			late.Envelope = a.Envelope
			p := appendClaudeIncoming(t, late, "surviving-writer-M1")
			if ok, e := st.ConfirmClaudeMessage(b.MessageID, p, time.Now()); ok || e != nil {
				t.Fatalf("late M1 proved M2 %v %v", ok, e)
			}
			p = appendClaudeIncoming(t, b, "actual-M2")
			if ok, e := st.ConfirmClaudeMessage(b.MessageID, p, time.Now()); !ok || e != nil {
				t.Fatalf("own M2 proof lost %v %v", ok, e)
			}
			if _, found, e := claudeinstance.FindProof(context.Background(), a); found || e != nil {
				t.Fatalf("wrong journal proved A %v %v", found, e)
			}
			m, _ := st.GetMessage(a.MessageID)
			if m.Status == "delivered" {
				t.Fatal("wrong-journal A delivered")
			}
		})
	}
}
