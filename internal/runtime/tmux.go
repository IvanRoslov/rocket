package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrSubmitUnconfirmed is returned by Inject when the text was pasted and
// Enter was sent the configured number of times, but submission could not
// be confirmed via the poll predicate before attempts were exhausted. This
// does NOT necessarily mean delivery failed — the text may well have been
// submitted; it means Inject could not verify it. Callers MUST NOT
// blindly re-inject on this error, since the original text may already
// have reached the target program: re-injecting risks sending a duplicate
// message. Callers should typically surface this to the operator or fall
// back to an out-of-band check (e.g. Capture) before deciding whether to
// retry.
var ErrSubmitUnconfirmed = errors.New("inject: submission unconfirmed after exhausting attempts")

// nameRE is the set of characters permitted in a session name. It is
// deliberately restrictive: session names are interpolated into tmux
// targets as "=name", and tmux target syntax treats many characters
// (":", ".", "%", etc.) specially.
var nameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// tmuxRuntime is the tmux-backed implementation of Runtime.
type tmuxRuntime struct{}

// NewTmux returns a Runtime that manages sessions via the tmux binary.
func NewTmux() Runtime {
	return &tmuxRuntime{}
}

func validateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid session name %q: must match %s", name, nameRE.String())
	}
	return nil
}

// sessionTarget returns the exact-match target for session-level tmux
// operations (new-session, has-session, kill-session, send-keys,
// paste-buffer).
func sessionTarget(name string) string {
	return "=" + name
}

// paneTarget returns the exact-match target for pane-level tmux
// operations (capture-pane).
func paneTarget(name string) string {
	return "=" + name + ":"
}

// runTmux runs `tmux <args...>` and returns stdout, stderr and any error
// from starting/waiting on the process. A non-zero exit status is
// reported via the returned error (as *exec.ExitError, wrapped).
func runTmux(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		err = fmt.Errorf("tmux %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr))
	}
	return stdout, stderr, err
}

func (t *tmuxRuntime) Create(ctx context.Context, spec CreateSpec) (Handle, error) {
	if err := validateName(spec.Name); err != nil {
		return Handle{}, err
	}

	launchPath := filepath.Join(spec.Dir, ".rocket-launch.sh")
	script := "#!/bin/sh\n" + spec.Command + "\nexec $SHELL -i\n"
	if err := os.WriteFile(launchPath, []byte(script), 0o700); err != nil {
		return Handle{}, fmt.Errorf("write launch script: %w", err)
	}

	args := []string{"new-session", "-d", "-s", spec.Name, "-c", spec.Dir}

	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}

	args = append(args, "sh .rocket-launch.sh")

	if _, _, err := runTmux(ctx, args...); err != nil {
		return Handle{}, fmt.Errorf("create session %q: %w", spec.Name, err)
	}

	// Multiple clients (e.g. the web dashboard's term WS attach and a
	// local `tmux attach`/`rocket attach`) can be attached to the same
	// session simultaneously — docs/03-daemon-api.md promises that works.
	// tmux's session-level window-size option controls which single size
	// the shared window is rendered at when clients disagree; the tmux
	// default, "latest", snaps the window to whichever client most
	// recently became active, including a *smaller* one. When that
	// happens, the window (and everything drawing into it, e.g. a
	// full-screen TUI) redraws at the smaller size while any other,
	// larger client's terminal emulator still believes it has the old
	// (bigger) dimensions — the client never receives escape codes for
	// the columns/rows outside the new smaller size, so stale content
	// from the previous, bigger frame is left behind uncleared. That
	// reads as corrupted, overlapping output. "largest" instead sizes
	// the window to the biggest attached client, so a smaller client
	// attaching only crops its own view (standard, harmless tmux
	// behavior) rather than shrinking everyone else's.
	// set-option's -t resolves an exact-match "=name" target at the
	// pane/window level (per tmux's target syntax), which fails here with
	// "no such window" since window-size is a session-scoped option;
	// unlike sessionTarget's other callers (send-keys, paste-buffer,
	// has-session, kill-session), this needs the bare session name.
	if _, _, err := runTmux(ctx, "set-option", "-t", spec.Name, "window-size", "largest"); err != nil {
		return Handle{}, fmt.Errorf("set window-size for session %q: %w", spec.Name, err)
	}

	return Handle{Name: spec.Name}, nil
}

// Inject clears any draft on the target pane's input line, pastes text,
// and presses Enter, retrying Enter up to maxAttempts times while polling
// for confirmation that the submit was processed. Submission is confirmed
// against confirmWindow — a true tail of the pane's bottom few rows, with
// unwritten trailing-blank padding trimmed first (see tailLines and
// trimTrailingBlank's docs for why this must be computed client-side) — as
// soon as EITHER of two independent signals fires:
//
//   - marker-absent: the last non-empty line of the injected text is no
//     longer present anywhere in confirmWindow, AND the marker was
//     observed there in the baseline (i.e. was actually rendered before
//     Enter). This is the primary signal for chat-style TUIs (e.g. Claude
//     Code): the submitted message gets echoed permanently into a
//     scrolling history area above the visible window, so it would never
//     "disappear" if checked against the whole pane — but the narrow
//     bottom-of-pane window (essentially just the input/prompt line) does
//     reliably go from "showing the draft" to "empty again" once
//     submitted. It also covers full-screen/alt-screen TUIs that redraw on
//     submit with the same surrounding line count and footer.
//   - count-growth: the number of non-blank lines in confirmWindow grew
//     versus the pre-Enter baseline. This covers simple echo-style
//     consumers (e.g. `cat`) where the submitted text lingers as an
//     echoed line, and cases where the marker never rendered in the first
//     place.
//
// If attempts are exhausted without either signal firing, Inject returns
// ErrSubmitUnconfirmed (wrapped with context) rather than a generic
// error, since by that point the text has very likely already been
// delivered to the target program even though Inject could not verify
// it — see ErrSubmitUnconfirmed's doc comment for the caller contract.
func (t *tmuxRuntime) Inject(ctx context.Context, h Handle, text string) error {
	if err := validateName(h.Name); err != nil {
		return err
	}

	// 1. Clear any existing draft on the input line. send-keys and
	// paste-buffer address a pane, not a session, so they need the
	// colon-suffixed pane target even though has-session/kill-session
	// accept the bare session target.
	if _, _, err := runTmux(ctx, "send-keys", "-t", paneTarget(h.Name), "C-u"); err != nil {
		return fmt.Errorf("clear draft: %w", err)
	}

	// 2. Load the text into a tmux buffer via a temp file, then paste it.
	tmpFile, err := os.CreateTemp(os.TempDir(), "rocket-inject-")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := tmpFile.Chmod(0o600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmpFile.WriteString(text); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	bufName := "rocket-" + h.Name
	if _, _, err := runTmux(ctx, "load-buffer", "-b", bufName, tmpPath); err != nil {
		return fmt.Errorf("load buffer: %w", err)
	}
	if _, _, err := runTmux(ctx, "paste-buffer", "-d", "-b", bufName, "-t", paneTarget(h.Name)); err != nil {
		return fmt.Errorf("paste buffer: %w", err)
	}

	// 3. Adaptive submit: press Enter, then poll until the draft is gone
	// from the visible tail (i.e. it was actually submitted). Some
	// programs / TUIs occasionally swallow the first Enter, so retry.
	if strings.TrimRight(text, "\n") == "" {
		// Nothing meaningful to verify; a single Enter is sufficient.
		_, _, err := runTmux(ctx, "send-keys", "-t", paneTarget(h.Name), "Enter")
		return err
	}

	const maxAttempts = 5
	const pollInterval = 300 * time.Millisecond
	const pollTimeout = 1500 * time.Millisecond

	marker := lastLine(text)

	// confirmWindow bounds every capture used for confirmation to a true
	// tail of the pane's bottom few rows — the input/prompt line plus a
	// little surrounding chrome. This is deliberately narrow: chat-style
	// TUIs (e.g. Claude Code) echo a submitted message permanently into a
	// scrolling history area, so checking the *whole* pane for the marker
	// would see it forever and never confirm submission. But a properly
	// bounded bottom-of-pane tail reliably distinguishes "marker is the
	// active, not-yet-submitted draft on the input line" (baseline) from
	// "marker has moved into history and the input line is empty/footer
	// chrome again" (submitted) — see tailLines's doc for why this must be
	// done client-side rather than via tmux's own -S/-E.
	const confirmWindow = 5

	// 3a. Pre-check: poll until the marker appears in the pane, up to
	// pollTimeout. This ensures the marker was actually rendered before we
	// proceed with Enter attempts. If the marker never appears (e.g. the
	// input was consumed instantly without echo), we'll proceed with Enter
	// and rely solely on count-growth for submission confirmation.
	markerSeen := false
	if marker != "" {
		deadline := time.Now().Add(pollTimeout)
		for {
			out, _, err := runTmux(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
			if err == nil && strings.Contains(tailLines(trimTrailingBlank(out), confirmWindow), marker) {
				markerSeen = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(pollInterval)
		}
	}

	baseline, _, err := runTmux(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
	if err != nil {
		return fmt.Errorf("capture baseline: %w", err)
	}
	baseCount := nonBlankLineCount(tailLines(trimTrailingBlank(baseline), confirmWindow))

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if _, _, err := runTmux(ctx, "send-keys", "-t", paneTarget(h.Name), "Enter"); err != nil {
			return fmt.Errorf("send Enter: %w", err)
		}

		deadline := time.Now().Add(pollTimeout)
		for {
			full, _, err := runTmux(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
			if err != nil {
				return fmt.Errorf("poll capture-pane: %w", err)
			}
			out := tailLines(trimTrailingBlank(full), confirmWindow)
			// marker-absent: the injected text's last line no longer
			// appears anywhere in the tail — the input box was cleared.
			// Handles full-screen/alt-screen TUIs that redraw with a
			// static line count and footer on submit. Only valid if
			// markerSeen is true (marker was rendered in the baseline).
			markerAbsent := markerSeen && marker != "" && !strings.Contains(out, marker)
			// count-growth: the tail gained non-blank lines vs baseline
			// — handles echo-style consumers (e.g. cat) where the
			// marker lingers in the tail, and also handles cases where
			// the marker never rendered.
			countGrowth := nonBlankLineCount(out) > baseCount
			if markerAbsent || countGrowth {
				return nil
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(pollInterval)
		}
	}

	return fmt.Errorf("%w: after %d attempts", ErrSubmitUnconfirmed, maxAttempts)
}

// lastLine returns the final non-blank line of s, or "" if s has no
// non-blank content.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// tailLines returns the last n lines of s. It exists because
// `tmux capture-pane -S -N` (without a matching -E) is NOT a "last N lines"
// tail: -S is an absolute offset from the top of the currently visible
// pane, and without -E the capture runs through to the bottom of the
// visible screen regardless of N — for a pane taller than N this yields
// (pane height + N) lines, not N. Pairing it with "-E -1" to bound the end
// was tried and found unreliable for alternate-screen TUIs (e.g. Claude
// Code), which sometimes report an empty capture for that range. Capturing
// generously via -S -N (or with no -S at all) and then trimming to exactly
// the last n lines here, in Go, sidesteps both problems.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// trimTrailingBlank drops trailing blank lines from s. A captured pane
// often has unwritten rows below the actual content (padding out to the
// full terminal height), and those don't count as "the tail" of anything —
// without trimming them first, tailLines on a lightly-used pane would
// return mostly (or entirely) blank output instead of the real content
// sitting above the padding.
func trimTrailingBlank(s string) string {
	lines := strings.Split(s, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[:end], "\n")
}

// nonBlankLineCount counts the non-blank lines in s.
func nonBlankLineCount(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// Capture returns (up to) the last `lines` lines of the pane's actual
// content. It captures generously via `-S -N` (best-effort extra
// scrollback — harmless no-op for alternate-screen TUIs that have none),
// then trims unwritten trailing-blank padding and bounds the result to
// exactly `lines` in Go; see tailLines and trimTrailingBlank for why doing
// this client-side is necessary rather than trusting tmux's own -S/-E
// bounds.
func (t *tmuxRuntime) Capture(ctx context.Context, h Handle, lines int) (string, error) {
	if err := validateName(h.Name); err != nil {
		return "", err
	}
	out, _, err := runTmux(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name), "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", fmt.Errorf("capture pane %q: %w", h.Name, err)
	}
	return tailLines(trimTrailingBlank(out), lines), nil
}

func (t *tmuxRuntime) Alive(ctx context.Context, h Handle) bool {
	if err := validateName(h.Name); err != nil {
		return false
	}
	_, _, err := runTmux(ctx, "has-session", "-t", sessionTarget(h.Name))
	return err == nil
}

func (t *tmuxRuntime) Destroy(ctx context.Context, h Handle) error {
	if err := validateName(h.Name); err != nil {
		return err
	}
	_, stderr, err := runTmux(ctx, "kill-session", "-t", sessionTarget(h.Name))
	if err != nil {
		if isNoSessionError(stderr) {
			return nil
		}
		return fmt.Errorf("destroy session %q: %w", h.Name, err)
	}
	return nil
}

func isNoSessionError(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "can't find session") ||
		strings.Contains(s, "no such session") ||
		strings.Contains(s, "session not found")
}

func (t *tmuxRuntime) AttachCommand(h Handle) []string {
	return []string{"tmux", "attach", "-t", sessionTarget(h.Name)}
}

func (t *tmuxRuntime) List(ctx context.Context) ([]string, error) {
	out, stderr, err := runTmux(ctx, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		if isNoServerError(stderr) || (strings.TrimSpace(out) == "" && strings.TrimSpace(stderr) == "") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return []string{}, nil
	}
	names := strings.Split(trimmed, "\n")
	return names, nil
}

func isNoServerError(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no server running") || strings.Contains(s, "error connecting")
}
