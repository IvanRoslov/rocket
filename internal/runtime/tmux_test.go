package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// uniqueName returns a session name matching ^[a-z0-9-]+$ that is unique
// per test invocation.
func uniqueName(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("rocket-test-%x%s", time.Now().UnixNano(), suffix)
}

// waitFor polls cond every 100ms until it returns true or the deadline
// (2s) is reached, then fails the test.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", msg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTmux_CreateInjectCaptureDestroy(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{
		Name:    name,
		Dir:     t.TempDir(),
		Command: "cat",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	if !rt.Alive(ctx, h) {
		t.Fatalf("expected session alive after Create")
	}

	if err := rt.Inject(ctx, h, "hello"); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		if err != nil {
			return false
		}
		return strings.Contains(out, "hello")
	}, "captured output to contain injected text")

	if err := rt.Destroy(ctx, h); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	waitFor(t, func() bool {
		return !rt.Alive(ctx, h)
	}, "session to be gone after Destroy")
}

func TestTmux_EnvVisible(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{
		Name:    name,
		Dir:     t.TempDir(),
		Command: `printf '%s\n' "$FOO"; exec cat`,
		Env:     map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		if err != nil {
			return false
		}
		return strings.Contains(out, "bar")
	}, "captured output to contain env value")
}

func TestTmux_PrefixMatchSafety(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	base := uniqueName(t, "")
	longer := base + "1"

	hBase, err := rt.Create(ctx, CreateSpec{Name: base, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}
	defer rt.Destroy(ctx, hBase)

	hLonger, err := rt.Create(ctx, CreateSpec{Name: longer, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create longer: %v", err)
	}
	defer rt.Destroy(ctx, hLonger)

	if err := rt.Destroy(ctx, hBase); err != nil {
		t.Fatalf("Destroy base: %v", err)
	}

	waitFor(t, func() bool { return !rt.Alive(ctx, hBase) }, "base session gone")

	if !rt.Alive(ctx, hLonger) {
		t.Fatalf("expected longer-named session to remain alive after destroying base (prefix-match bug)")
	}
}

func TestTmux_ListContainsCreated(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	names, err := rt.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected List() to contain %q, got %v", name, names)
	}
}

func TestTmux_DestroyIdempotent(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := rt.Destroy(ctx, h); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := rt.Destroy(ctx, h); err != nil {
		t.Fatalf("second Destroy (idempotent) should not error: %v", err)
	}
}

func TestTmux_InvalidNameRejected(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	h := Handle{Name: "Invalid Name!"}

	if _, err := rt.Create(ctx, CreateSpec{Name: h.Name, Dir: t.TempDir(), Command: "cat"}); err == nil {
		t.Fatalf("expected Create to reject invalid name")
	}
	if err := rt.Inject(ctx, h, "x"); err == nil {
		t.Fatalf("expected Inject to reject invalid name")
	}
	if _, err := rt.Capture(ctx, h, 10); err == nil {
		t.Fatalf("expected Capture to reject invalid name")
	}
	if rt.Alive(ctx, h) {
		t.Fatalf("expected Alive to return false for invalid name")
	}
	if err := rt.Destroy(ctx, h); err == nil {
		t.Fatalf("expected Destroy to reject invalid name")
	}
}
