package pi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func confirmDelivery(t *testing.T, worktree, text string, since int64) (bool, time.Time, error) {
	t.Helper()
	return New().(agent.DeliveryConfirmer).ConfirmDelivered(
		context.Background(),
		agent.ActivityRef{SessionID: "s1", WorktreePath: worktree},
		text,
		time.UnixMilli(since),
	)
}

// TestPiNativeProofRequiresTargetHistory pins the native binding: a matching
// text in another worktree's history is not proof for this launch.
func TestPiNativeProofRequiresTargetHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	targetWorktree := t.TempDir()
	otherWorktree := t.TempDir()
	target := writeSessionFile(t, home, targetWorktree, "2026-10-29_s1.jsonl")
	other := writeSessionFile(t, home, otherWorktree, "2026-10-29_s1.jsonl")
	if err := os.WriteFile(target, []byte(piMessageLine(t, "u1", "", "user", "unrelated", 2000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(piMessageLine(t, "u1", "", "user", "wanted", 2000)), 0o644); err != nil {
		t.Fatal(err)
	}

	found, _, err := confirmDelivery(t, targetWorktree, "wanted", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a foreign worktree history confirmed delivery")
	}

	if err := os.WriteFile(target, []byte(
		piMessageLine(t, "u1", "", "user", "unrelated", 2000)+
			piMessageLine(t, "u2", "u1", "user", "wanted", 3000),
	), 0o644); err != nil {
		t.Fatal(err)
	}
	found, at, err := confirmDelivery(t, targetWorktree, "wanted", 1000)
	if err != nil || !found || !at.Equal(time.UnixMilli(3000)) {
		t.Fatalf("target history proof = %v at %v, err %v", found, at, err)
	}
}

// TestPiNativeEqualTextProofIsOneToOne shows the Pi confirmer can supply two
// distinct proofs for two messages with equal text; the core queue consumes
// each record for only one message (TestNativeDuplicateTextUsesDistinctMessageIDs).
func TestPiNativeEqualTextProofIsOneToOne(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "same text", 2000) +
		piMessageLine(t, "u2", "u1", "user", "same text", 3000)
	worktree, _ := fakePiSession(t, wire, "s1", piOldMtime)

	found, first, err := confirmDelivery(t, worktree, "same text", 1000)
	if err != nil || !found || !first.Equal(time.UnixMilli(2000)) {
		t.Fatalf("first proof = %v at %v, err %v", found, first, err)
	}
	found, second, err := confirmDelivery(t, worktree, "same text", 2000)
	if err != nil || !found || !second.Equal(time.UnixMilli(3000)) {
		t.Fatalf("second proof = %v at %v, err %v", found, second, err)
	}
	if found, _, err := confirmDelivery(t, worktree, "same text", 3000); err != nil || found {
		t.Fatalf("a consumed record re-confirmed a later send: %v %v", found, err)
	}

	single := piMessageLine(t, "u1", "", "user", "only once", 2000)
	singleWorktree, _ := fakePiSession(t, single, "s1", piOldMtime)
	if found, _, err := confirmDelivery(t, singleWorktree, "only once", 2000); err != nil || found {
		t.Fatalf("one record confirmed itself after its send boundary: %v %v", found, err)
	}
}

// TestPiNativeUnknownSurvivesRecover pins the proof window used after an
// unknown native outcome: only a record strictly after the saved inject_at
// confirms, and the proof is still readable on a later (recovery) scan.
func TestPiNativeUnknownSurvivesRecover(t *testing.T) {
	wire := piMessageLine(t, "old", "", "user", "hello", 500) +
		piMessageLine(t, "new", "old", "user", "hello", 2000)
	worktree, _ := fakePiSession(t, wire, "s1", piOldMtime)

	for attempt := 0; attempt < 2; attempt++ {
		found, at, err := confirmDelivery(t, worktree, "hello", 1000)
		if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
			t.Fatalf("recovery proof %d = %v at %v, err %v", attempt, found, at, err)
		}
	}
	if found, _, err := confirmDelivery(t, worktree, "hello", 2000); err != nil || found {
		t.Fatalf("record at the send boundary confirmed: %v %v", found, err)
	}
}

// TestPiEditorEchoDoesNotConfirmDelivery covers the native rule that queued
// editor text, custom entries and assistant echoes are not history proof.
func TestPiEditorEchoDoesNotConfirmDelivery(t *testing.T) {
	wire := `{"type":"custom","id":"c1","parentId":null,"timestamp":"2026-11-01T10:00:01.000Z","text":"Steering: reply HELLO_NATIVE"}` + "\n" +
		piMessageLine(t, "a1", "c1", "assistant", "HELLO_NATIVE", 2000) +
		`{"type":"custom_message","id":"c2","parentId":"a1","timestamp":"2026-11-01T10:00:03.000Z","content":"HELLO_NATIVE"}` + "\n"
	worktree, _ := fakePiSession(t, wire, "s1", piOldMtime)

	if found, _, err := confirmDelivery(t, worktree, "HELLO_NATIVE", 1000); err != nil || found {
		t.Fatalf("editor/assistant echo confirmed delivery: %v %v", found, err)
	}
}
