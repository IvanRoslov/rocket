package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestTmux_ScrollHistory(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()
	h, err := rt.Create(ctx, CreateSpec{
		Name:    uniqueName(t, "-scroll"),
		Dir:     t.TempDir(),
		Command: "seq 1 500; exec cat",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	modeAndPosition := func() string {
		t.Helper()
		out, _, err := runTmux(ctx, "display-message", "-p", "-t", paneTarget(h.Name), "#{pane_in_mode} #{scroll_position}")
		if err != nil {
			t.Fatalf("display-message: %v", err)
		}
		return strings.TrimSpace(out)
	}
	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		return err == nil && strings.Contains(out, "500")
	}, "500 lines of pane output")

	if err := rt.ScrollHistory(ctx, h, -10); err != nil {
		t.Fatalf("ScrollHistory(-10): %v", err)
	}
	if got := modeAndPosition(); got != "1 10" {
		t.Fatalf("after scroll up: mode and position = %q, want %q", got, "1 10")
	}

	if err := rt.ScrollHistory(ctx, h, 10); err != nil {
		t.Fatalf("ScrollHistory(10): %v", err)
	}
	if got := modeAndPosition(); got != "0" {
		t.Fatalf("after scroll down: mode and position = %q, want mode 0", got)
	}
	if err := rt.ScrollHistory(ctx, h, 5); err != nil {
		t.Fatalf("ScrollHistory(5) outside copy-mode: %v", err)
	}
	if got := modeAndPosition(); got != "0" {
		t.Fatalf("scroll down outside copy-mode entered it: %q", got)
	}

	if err := rt.ScrollHistory(ctx, h, -10); err != nil {
		t.Fatalf("second ScrollHistory(-10): %v", err)
	}
	if err := rt.ExitHistory(ctx, h); err != nil {
		t.Fatalf("ExitHistory: %v", err)
	}
	if got := modeAndPosition(); got != "0" {
		t.Fatalf("after ExitHistory: mode and position = %q, want mode 0", got)
	}
	if err := rt.ExitHistory(ctx, h); err != nil {
		t.Fatalf("repeated ExitHistory: %v", err)
	}
}
