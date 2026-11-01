package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// These tests cover the additive native-only strict proof from Spec3/Plan3:
// one user record proves the expected text only when its complete joined text
// content equals that text after the existing whitespace normalization. The
// legacy ConfirmDelivered/holdsLines behavior is exercised unchanged.

const nativeProofEnvelope = "[rocket #123] [from helper] Продолжай"

func nativePiPartsLine(t *testing.T, id, parentID, role string, parts []string, at int64) string {
	t.Helper()
	content := make([]map[string]string, 0, len(parts))
	for _, part := range parts {
		content = append(content, map[string]string{"type": "text", "text": part})
	}
	rec := map[string]any{
		"type":      "message",
		"id":        id,
		"parentId":  parentID,
		"timestamp": time.UnixMilli(at).UTC().Format(time.RFC3339Nano),
		"message":   map[string]any{"role": role, "content": content},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func nativeConfirmStrict(t *testing.T, wire, text string, since int64) (bool, time.Time, error) {
	t.Helper()
	wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
	return New().(*Pi).ConfirmNativeDelivered(
		context.Background(),
		agent.ActivityRef{SessionID: "s1", WorktreePath: wt},
		text,
		time.UnixMilli(since),
	)
}

func TestPiNativeProofRequiresWholeUserRecord(t *testing.T) {
	t.Run("own complete signed record proves", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 2000)
		found, at, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
			t.Fatalf("got %v %v %v", found, at, err)
		}
	})

	t.Run("quoted foreign envelope on a later line does not prove", func(t *testing.T) {
		quoted := "[rocket #124] [from helper] own message\n" + nativeProofEnvelope
		wire := piMessageLine(t, "u1", "", "user", quoted, 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a quoted envelope inside a larger record proved it", found, err)
		}
	})

	t.Run("expected envelope plus extra text part does not prove", func(t *testing.T) {
		wire := nativePiPartsLine(t, "u1", "", "user", []string{nativeProofEnvelope, "extra text"}, 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a text part with extra content proved it", found, err)
		}
	})

	t.Run("envelope after an extra text part does not prove", func(t *testing.T) {
		wire := nativePiPartsLine(t, "u1", "", "user", []string{"extra text", nativeProofEnvelope}, 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; an envelope in one part of a larger record proved it", found, err)
		}
	})

	t.Run("expected envelope split across text parts proves", func(t *testing.T) {
		wire := nativePiPartsLine(t, "u1", "", "user", []string{"[rocket #123]", "[from helper] Продолжай"}, 2000)
		found, at, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
			t.Fatalf("got %v %v %v; a whole record split across text parts must prove", found, at, err)
		}
	})

	t.Run("multiline Unicode with whitespace normalization proves", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", "[rocket #123] Привет,\n   мир", 2000)
		found, _, err := nativeConfirmStrict(t, wire, "[rocket #123]  Привет,   мир", 1000)
		if err != nil || !found {
			t.Fatalf("got %v %v", found, err)
		}
	})

	t.Run("foreign ID does not prove", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", "[rocket #124] [from helper] Продолжай", 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; another ID proved the message", found, err)
		}
	})

	t.Run("assistant echo does not prove", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "assistant", nativeProofEnvelope, 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; an assistant record proved the message", found, err)
		}
	})

	t.Run("editor or custom echo does not prove", func(t *testing.T) {
		at := time.UnixMilli(2000).UTC().Format(time.RFC3339Nano)
		wire := fmt.Sprintf(`{"type":"custom","id":"c1","parentId":"","timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":%q}]}}`+"\n", at, nativeProofEnvelope)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a custom/editor echo proved the message", found, err)
		}
	})

	t.Run("record at the boundary does not prove", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 1000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a record at the boundary proved the message", found, err)
		}
	})

	t.Run("number-only does not prove a nonempty body", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", "[rocket #123] ", 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a number-only record proved a nonempty body", found, err)
		}
	})

	t.Run("empty body service header proves", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", "[rocket #123] ", 2000)
		found, at, err := nativeConfirmStrict(t, wire, "[rocket #123] ", 1000)
		if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
			t.Fatalf("got %v %v %v", found, at, err)
		}
	})

	t.Run("strictly older matching record does not prove", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 500) +
			piMessageLine(t, "u2", "u1", "user", "other", 2000)
		found, _, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || found {
			t.Fatalf("got %v %v; a record strictly before the boundary proved the message", found, err)
		}
	})

	t.Run("earliest matching record is reported", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 2000) +
			piMessageLine(t, "u2", "u1", "user", nativeProofEnvelope, 3000)
		found, at, err := nativeConfirmStrict(t, wire, nativeProofEnvelope, 1000)
		if err != nil || !found || !at.Equal(time.UnixMilli(2000)) {
			t.Fatalf("got %v %v %v; want the earliest matching record", found, at, err)
		}
	})
}

func TestPiNativeProofContextAndHistory(t *testing.T) {
	t.Run("cancelled context never proves", func(t *testing.T) {
		wire := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 2000)
		wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		found, _, err := New().(*Pi).ConfirmNativeDelivered(
			ctx,
			agent.ActivityRef{SessionID: "s1", WorktreePath: wt},
			nativeProofEnvelope,
			time.UnixMilli(1000),
		)
		if !errors.Is(err, context.Canceled) || found {
			t.Fatalf("got %v %v; want context.Canceled and not proved", found, err)
		}
	})

	t.Run("missing history never proves", func(t *testing.T) {
		t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
		found, _, err := New().(*Pi).ConfirmNativeDelivered(
			context.Background(),
			agent.ActivityRef{SessionID: "missing", WorktreePath: t.TempDir()},
			nativeProofEnvelope,
			time.UnixMilli(1000),
		)
		if found || !errors.Is(err, agent.ErrNoSignal) {
			t.Fatalf("got %v %v; want ErrNoSignal and not proved", found, err)
		}
	})

	t.Run("read error never proves", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(home, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PI_CODING_AGENT_DIR", home)
		found, _, err := New().(*Pi).ConfirmNativeDelivered(
			context.Background(),
			agent.ActivityRef{SessionID: "s1", WorktreePath: t.TempDir()},
			nativeProofEnvelope,
			time.UnixMilli(1000),
		)
		if err == nil || errors.Is(err, agent.ErrNoSignal) {
			t.Fatalf("error = %v, want a real session-store read failure", err)
		}
		if found {
			t.Fatal("a read error proved the message")
		}
	})

	t.Run("wrong session history never proves", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("PI_CODING_AGENT_DIR", home)
		wt := t.TempDir()
		unrelated := piMessageLine(t, "u1", "", "user", "unrelated", 2000)
		matching := piMessageLine(t, "u1", "", "user", nativeProofEnvelope, 2000)
		if err := os.WriteFile(writeSessionFile(t, home, wt, "2026_s1.jsonl"), []byte(unrelated), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(writeSessionFile(t, home, wt, "2026_other.jsonl"), []byte(matching), 0o644); err != nil {
			t.Fatal(err)
		}
		found, _, err := New().(*Pi).ConfirmNativeDelivered(
			context.Background(),
			agent.ActivityRef{SessionID: "s1", WorktreePath: wt},
			nativeProofEnvelope,
			time.UnixMilli(1000),
		)
		if err != nil || found {
			t.Fatalf("got %v %v; another session proved the message", found, err)
		}
	})
}

func TestPiNativeProofLeavesLegacyConfirmerUnchanged(t *testing.T) {
	wire := piMessageLine(t, "u1", "", "user", "first message\n"+nativeProofEnvelope, 2000)
	wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
	ref := agent.ActivityRef{SessionID: "s1", WorktreePath: wt}

	legacyFound, _, err := New().(agent.DeliveryConfirmer).ConfirmDelivered(
		context.Background(), ref, nativeProofEnvelope, time.UnixMilli(1000),
	)
	if err != nil || !legacyFound {
		t.Fatalf("legacy holdsLines changed: got %v %v", legacyFound, err)
	}

	strictFound, _, err := New().(*Pi).ConfirmNativeDelivered(
		context.Background(), ref, nativeProofEnvelope, time.UnixMilli(1000),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strictFound {
		t.Fatal("strict native proof matched a line inside a larger record")
	}
}
