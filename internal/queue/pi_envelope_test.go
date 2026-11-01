package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Test-only strict capability for the existing scripted fakeAgent. The
// production Pi adapter is exercised through nativePiProofConfirmer; the fake
// already matches the complete normalized text, so delegating is exact.
func (a *fakeAgent) ConfirmNativeDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	return a.ConfirmDelivered(ctx, ref, text, since)
}

func TestNativePiEnvelopeFormat(t *testing.T) {
	cases := []struct {
		name     string
		id       int64
		prepared string
		want     string
	}{
		{"sender prefix", 123, "[from helper] Продолжай", "[rocket #123] [from helper] Продолжай"},
		{"unknown sender", 123, "Продолжай", "[rocket #123] Продолжай"},
		{"empty body", 123, "", "[rocket #123] "},
		{"similar header inside body stays absolute second", 123, "[rocket #999] внутри", "[rocket #123] [rocket #999] внутри"},
		{"different IDs differ", 124, "Продолжай", "[rocket #124] Продолжай"},
		{"Unicode multiline preserved", 7, "Привет,\nмир", "[rocket #7] Привет,\nмир"},
		{"canonical decimal", 1, "x", "[rocket #1] x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativePiEnvelope(tc.id, tc.prepared); got != tc.want {
				t.Fatalf("nativePiEnvelope(%d, %q) = %q, want %q", tc.id, tc.prepared, got, tc.want)
			}
		})
	}

	first := nativePiEnvelope(123, "same body")
	second := nativePiEnvelope(124, "same body")
	if first == second {
		t.Fatalf("equal bodies with different IDs produced the same envelope: %q", first)
	}
}

func TestNativePiEnvelopePreparedVariants(t *testing.T) {
	h := newTestQueue(t)
	sess := store.Session{ID: "native", WorktreePath: t.TempDir()}
	longBody := strings.Repeat("длинный текст ", 300)
	msg := store.Message{ID: 77, FromSession: "sender", ToSession: "native", Body: longBody}

	pointer := h.q.prepareText(msg, sess)
	if pointer == longBody || !strings.Contains(pointer, "msg-77.md") {
		t.Fatalf("prepareText did not produce the inbox pointer: %q", pointer)
	}
	inbox := filepath.Join(sess.WorktreePath, inboxRelPath(msg.ID))
	content, err := os.ReadFile(inbox)
	if err != nil {
		t.Fatalf("read inbox file: %v", err)
	}
	if string(content) != longBody {
		t.Fatalf("inbox content = %q, want the original Body byte-for-byte", string(content))
	}
	if got, want := h.q.nativePiInjectedText(msg, sess), nativePiEnvelope(msg.ID, pointer); got != want {
		t.Fatalf("pointer native text = %q, want %q", got, want)
	}
	// The generic read-only reconstruction is unchanged.
	if got := h.q.injectedText(msg, sess); got != pointer {
		t.Fatalf("legacy injectedText changed: %q != %q", got, pointer)
	}

	// A missing inbox file reconstructs the existing full-body fallback; it is
	// a different string, so a pointer record cannot match it.
	if err := os.Remove(inbox); err != nil {
		t.Fatal(err)
	}
	fullBody := formatBody(msg)
	if got, want := h.q.nativePiInjectedText(msg, sess), nativePiEnvelope(msg.ID, fullBody); got != want {
		t.Fatalf("fallback native text = %q, want %q", got, want)
	}
	if h.q.nativePiInjectedText(msg, sess) == nativePiEnvelope(msg.ID, pointer) {
		t.Fatal("missing pointer reconstruction equals the pointer envelope")
	}

	short := store.Message{ID: 78, FromSession: "sender", ToSession: "native", Body: "коротко"}
	if got, want := h.q.nativePiInjectedText(short, sess), nativePiEnvelope(78, "[from sender] коротко"); got != want {
		t.Fatalf("short native text = %q, want %q", got, want)
	}
	empty := store.Message{ID: 79, ToSession: "native", Body: ""}
	if got, want := h.q.nativePiInjectedText(empty, sess), "[rocket #79] "; got != want {
		t.Fatalf("empty-body native text = %q, want %q", got, want)
	}
}

type strictProofFake struct {
	strictCalls int
	legacyCalls int
	result      bool
	at          time.Time
	err         error
}

func (f *strictProofFake) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	f.legacyCalls++
	return false, time.Time{}, errors.New("legacy matcher must not be used by the native adapter")
}

func (f *strictProofFake) ConfirmNativeDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	f.strictCalls++
	return f.result, f.at, f.err
}

type legacyOnlyFake struct {
	calls int
}

func (f *legacyOnlyFake) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	f.calls++
	return true, time.Time{}, nil
}

func TestNativePiProofAdapter(t *testing.T) {
	if got := nativePiProofConfirmer(nil); got != nil {
		t.Fatalf("nil base produced %#v, want nil", got)
	}
	legacy := &legacyOnlyFake{}
	if got := nativePiProofConfirmer(legacy); got != nil {
		t.Fatalf("legacy-only base produced %#v, want nil (no legacy fallback)", got)
	}

	at := time.UnixMilli(2000)
	strict := &strictProofFake{result: true, at: at}
	adapter := nativePiProofConfirmer(strict)
	if adapter == nil {
		t.Fatal("strict base produced a nil adapter")
	}
	found, gotAt, err := adapter.ConfirmDelivered(context.Background(), agent.ActivityRef{SessionID: "s1"}, "[rocket #1] x", time.UnixMilli(1000))
	if err != nil || !found || !gotAt.Equal(at) {
		t.Fatalf("adapter result = %v %v %v", found, gotAt, err)
	}
	if strict.strictCalls != 1 || strict.legacyCalls != 0 {
		t.Fatalf("adapter calls: strict=%d legacy=%d, want the strict method only", strict.strictCalls, strict.legacyCalls)
	}
	if legacy.calls != 0 {
		t.Fatalf("legacy confirmer was called %d times", legacy.calls)
	}
}
