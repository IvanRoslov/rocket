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

// TestTmux_CreateSetsWindowSizeLargest guards against a regression of the
// multi-client rendering corruption bug: tmux's default window-size option
// ("latest") snaps the shared window to whichever attached client most
// recently became active, even if that client is smaller — shrinking the
// window out from under any other, larger client without it ever being
// told, which leaves stale content behind and reads as corrupted,
// overlapping output. Create must set window-size to "largest" so the
// window always matches the biggest attached client instead.
func TestTmux_CreateSetsWindowSizeLargest(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	out, _, err := runTmux(ctx, "show-options", "-t", name, "window-size")
	if err != nil {
		t.Fatalf("show-options window-size: %v", err)
	}
	if got := strings.TrimSpace(out); got != "window-size largest" {
		t.Fatalf("expected window-size largest, got %q", got)
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

// TestTmux_InjectPackedTUI_MarkerAbsent reproduces a reviewer-reported
// false-negative: a full-screen (alt-screen) TUI that, on submit,
// redraws the exact same number of lines with the exact same static
// footer, only clearing the input box. Under the old detection
// (line-count growth OR last-line change) this scenario false-negatives
// because neither line count nor last line changes across the redraw.
// The new marker-absent signal must catch it: the injected text no
// longer appears in the tail after redraw.
func TestTmux_InjectPackedTUI_MarkerAbsent(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	// A tiny alt-screen "TUI": fills the screen with numbered lines plus
	// a static footer, reads a line, and on submit redraws the identical
	// screen (same line count, same footer) with the input box cleared.
	script := `printf '\033[?1049h'
redraw() {
  printf '\033[2J\033[H'
  i=1
  while [ "$i" -le 8 ]; do
    printf 'line %d\n' "$i"
    i=$((i+1))
  done
  printf -- '---static-footer---\n'
  printf '> '
}
redraw
while IFS= read -r line; do
  redraw
done`

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{
		Name:    name,
		Dir:     t.TempDir(),
		Command: script,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	// Let the alt-screen render before injecting.
	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		if err != nil {
			return false
		}
		return strings.Contains(out, "---static-footer---")
	}, "packed TUI to render its initial screen")

	done := make(chan error, 1)
	go func() {
		done <- rt.Inject(ctx, h, "hello")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Inject: expected success via marker-absent detection, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Inject did not return within timeout (likely burned all retries)")
	}

	// The screen should still show the same static content (redraw
	// happened, not a scroll/append), confirming this was genuinely the
	// packed-TUI redraw case rather than count-growth.
	out, err := rt.Capture(ctx, h, 20)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !strings.Contains(out, "---static-footer---") {
		t.Fatalf("expected footer to still be present after submit, got: %q", out)
	}
	if strings.Contains(out, "hello") {
		t.Fatalf("expected injected text to be cleared from input box after submit, got: %q", out)
	}
}

// TestTmux_InjectChatHistoryTUI_MarkerPersistsButConfirms reproduces the
// bug found running rocket's phase-2 E2E against real Claude Code
// sessions: a chat-style TUI that permanently echoes every submitted
// message into a scrolling history area above a static footer (so the
// marker text is *never* absent from the pane as a whole — it stays
// visible in history forever) and whose total on-screen non-blank line
// count stays pinned once the history area is full (old lines are pushed
// off-screen as new ones are appended, so a whole-pane line count doesn't
// grow either). Confirmation must still succeed by noticing the marker
// leave the narrow bottom-of-pane input/footer window, without needing
// the marker to vanish from the pane, or the total count to grow.
func TestTmux_InjectChatHistoryTUI_MarkerPersistsButConfirms(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()

	// A tiny alt-screen "chat TUI": maintains a fixed-size (3-line)
	// scrolling history window of everything ever submitted, above a
	// static separator/prompt footer. Submitted lines never disappear
	// from the pane (they scroll within the fixed history window and the
	// oldest one is dropped, keeping total on-screen line count pinned),
	// mirroring Claude Code's behavior.
	script := `printf '\033[?1049h'
touch /tmp/hist.$$
redraw() {
  printf '\033[2J\033[H'
  cat /tmp/hist.$$
  printf -- '---static-footer---\n'
  printf '> '
}
redraw
while IFS= read -r line; do
  printf '%s\n' "$line" >> /tmp/hist.$$
  tail -n 3 /tmp/hist.$$ > /tmp/hist.$$.tmp && mv /tmp/hist.$$.tmp /tmp/hist.$$
  redraw
done
rm -f /tmp/hist.$$`

	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{
		Name:    name,
		Dir:     t.TempDir(),
		Command: script,
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
		return strings.Contains(out, "---static-footer---")
	}, "chat TUI to render its initial screen")

	done := make(chan error, 1)
	go func() {
		done <- rt.Inject(ctx, h, "hello-marker")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Inject: expected success, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Inject did not return within timeout (likely burned all retries)")
	}

	// The marker legitimately remains visible in the pane (it's now part
	// of "history"), but must no longer be on the active input line.
	out, err := rt.Capture(ctx, h, 20)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !strings.Contains(out, "hello-marker") {
		t.Fatalf("expected marker to remain visible in history, got: %q", out)
	}
	if strings.Contains(out, "> hello-marker") {
		t.Fatalf("expected marker to have left the input line, got: %q", out)
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

// TestTmux_Inject_SettlePauseForLargeText verifies the defense-in-depth
// settle pause: Inject should invoke settleFn before the first Enter when
// the injected text has more than 20 lines, and must NOT invoke it for
// short text, since the pause exists specifically to give a TUI extra time
// to register a large paste before submission (see Inject's settle-pause
// comment for the race it guards against).
func TestTmux_Inject_SettlePauseForLargeText(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()

	t.Run("21 lines invokes settleFn", func(t *testing.T) {
		var calls []time.Duration
		rt := &tmuxRuntime{settleFn: func(d time.Duration) { calls = append(calls, d) }}
		name := uniqueName(t, "")
		h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer rt.Destroy(ctx, h)

		lines := make([]string, 21)
		for i := range lines {
			lines[i] = fmt.Sprintf("line %d", i)
		}
		text := strings.Join(lines, "\n")

		// The settle pause is invoked before the confirmation/Enter loop,
		// independent of whether submission is ultimately confirmed; cat
		// echoes multi-line pastes idiosyncratically (each embedded newline
		// acts like its own Enter), so don't assert on Inject's error here —
		// only on whether the pause fired.
		_ = rt.Inject(ctx, h, text)

		if len(calls) != 1 {
			t.Fatalf("settleFn called %d times, want 1", len(calls))
		}
		want := 200*time.Millisecond + 21*5*time.Millisecond
		if calls[0] != want {
			t.Errorf("settle duration = %v, want %v", calls[0], want)
		}
	})

	t.Run("5 lines does not invoke settleFn", func(t *testing.T) {
		var calls []time.Duration
		rt := &tmuxRuntime{settleFn: func(d time.Duration) { calls = append(calls, d) }}
		name := uniqueName(t, "")
		h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer rt.Destroy(ctx, h)

		lines := make([]string, 5)
		for i := range lines {
			lines[i] = fmt.Sprintf("line %d", i)
		}
		text := strings.Join(lines, "\n")

		_ = rt.Inject(ctx, h, text)

		if len(calls) != 0 {
			t.Fatalf("settleFn called %d times, want 0", len(calls))
		}
	})
}

// TestTmux_Inject_SettlePauseCappedAt2s verifies the settle pause is capped
// at 2s even for very large texts.
func TestTmux_Inject_SettlePauseCappedAt2s(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()

	var calls []time.Duration
	rt := &tmuxRuntime{settleFn: func(d time.Duration) { calls = append(calls, d) }}
	name := uniqueName(t, "")
	h, err := rt.Create(ctx, CreateSpec{Name: name, Dir: t.TempDir(), Command: "cat"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	lines := make([]string, 500) // 200ms + 500*5ms = 2.7s, should cap to 2s
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	text := strings.Join(lines, "\n")

	_ = rt.Inject(ctx, h, text)

	if len(calls) != 1 {
		t.Fatalf("settleFn called %d times, want 1", len(calls))
	}
	if calls[0] != 2*time.Second {
		t.Errorf("settle duration = %v, want capped 2s", calls[0])
	}
}
