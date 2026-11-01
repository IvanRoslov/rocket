package store

import (
	"encoding/json"
	"strings"
	"testing"
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
