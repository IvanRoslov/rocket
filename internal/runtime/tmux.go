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
type tmuxRuntime struct {
	// settleFn is called with a duration to pause before the first Enter
	// attempt on a large paste (see the settle-pause comment in Inject).
	// Defaults to time.Sleep; overridable by tests to observe the call
	// without actually waiting.
	settleFn func(time.Duration)
}

// NewTmux returns a Runtime that manages sessions via the tmux binary.
func NewTmux() Runtime {
	return &tmuxRuntime{settleFn: time.Sleep}
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
	// tmux fundamentally renders one window at ONE size; when attached
	// clients disagree, someone gets a cropped (and, as the cursor moves,
	// horizontally panned) view, which reads as clipped lines and stale
	// redraw fragments. The client-size policy (docs/03-daemon-api.md
	// «Размер окна») is: the WEB terminal is the primary surface and must
	// always render full-width, so while a web terminal is attached the
	// daemon pins the window to the web client's exact size via
	// PinWindowSize (window-size manual), and any degradation (crop/pad)
	// goes to local clients. Outside that, "latest" — the tmux default,
	// set explicitly here for determinism — lets local clients drive the
	// window natively. ("largest" was tried in cd293f3 and rejected: a
	// wider local client permanently cropped the web view.)
	// set-option's -t resolves an exact-match "=name" target at the
	// pane/window level (per tmux's target syntax), which fails here with
	// "no such window" since window-size is a session-scoped option;
	// unlike sessionTarget's other callers (send-keys, paste-buffer,
	// has-session, kill-session), this needs the bare session name.
	if _, _, err := runTmux(ctx, "set-option", "-t", spec.Name, "window-size", "latest"); err != nil {
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

	// Defense-in-depth against a live-production race reproduced 2026-07-19:
	// Claude Code's TUI can intermittently lose a large paste even though
	// tmux's paste-buffer injection itself succeeds — the paste appears to
	// land, but the TUI hasn't finished registering it into its input
	// buffer by the time Enter arrives, so Enter submits whatever partial
	// (or empty) state the TUI has registered so far, silently dropping
	// the rest. Giving the TUI a short settle pause before the first Enter
	// — scaled by paste size, since larger pastes take proportionally
	// longer to register — gives it time to catch up. Only worth paying
	// for pastes long enough that the race is plausible; short injections
	// are unaffected and get no pause.
	if n := strings.Count(text, "\n") + 1; n > 20 {
		settle := 200*time.Millisecond + time.Duration(n)*5*time.Millisecond
		if settle > 2*time.Second {
			settle = 2 * time.Second
		}
		t.settleFn(settle)
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Re-capture right before a RETRY Enter: if the agent has
			// meanwhile replaced its composer with an interactive quiz
			// widget, the draft was necessarily submitted (the widget only
			// renders mid-turn) and another Enter would press a quiz
			// button instead — see LooksLikeQuizWidget.
			if out, _, err := runTmux(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name)); err == nil {
				if LooksLikeQuizWidget(tailLines(trimTrailingBlank(out), confirmWindow)) {
					return nil
				}
			}
		}
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
			// Quiz-widget guard: Claude Code's AskUserQuestion widget
			// replacing the composer proves the draft was submitted (the
			// widget only renders while the agent is processing a turn),
			// even when neither marker-absent nor count-growth fired —
			// e.g. the submitted message's echo keeps the marker line in
			// the tail while the agent thinks. Confirm WITHOUT pressing
			// Enter again: a further Enter would land on the widget and
			// select a quiz option (live incident, 2026-07-19). Exported: the monitor reuses it
			// as the cancelled-quiz backstop (see monitor.pollQuiz).
			if LooksLikeQuizWidget(out) {
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

// SendKeys sends one logical key to the pane: a tmux key name (e.g.
// "Enter", "Tab", "Down", "Space", or a bare digit character) when literal
// is false, or raw literal text via send-keys -l when literal is true. See
// the Runtime.SendKeys doc comment for the contract.
func (t *tmuxRuntime) SendKeys(ctx context.Context, h Handle, key string, literal bool) error {
	if err := validateName(h.Name); err != nil {
		return err
	}
	args := []string{"send-keys", "-t", paneTarget(h.Name)}
	if literal {
		// "--" ends flag parsing so literal text starting with "-" (e.g.
		// "-foo") is never mistaken for a send-keys flag, and
		// escapeTrailingSemicolon neutralizes tmux's command-sequence
		// separator, which it special-cases only for a trailing,
		// unescaped ";" argument (see escapeTrailingSemicolon's doc
		// comment and TestSendKeys_LiteralSafety).
		args = append(args, "-l", "--", escapeTrailingSemicolon(key))
	} else {
		args = append(args, key)
	}
	if _, _, err := runTmux(ctx, args...); err != nil {
		return fmt.Errorf("send-keys %q (literal=%v) to %q: %w", key, literal, h.Name, err)
	}
	return nil
}

// escapeTrailingSemicolon escapes s's trailing ";" as "\;" if present and
// not already escaped. tmux treats a bare trailing semicolon in a
// send-keys argument as its command-sequence separator (letting multiple
// tmux commands be chained on one command line) even under "-l" literal
// mode and even behind "--"; verified empirically (tmux 3.6a) that
// `send-keys -l -- 'bar;'` sends only "bar" while
// `send-keys -l -- 'bar\;'` sends the literal "bar;". A semicolon anywhere
// but the last character is unaffected and needs no escaping.
func escapeTrailingSemicolon(s string) string {
	if len(s) == 0 || s[len(s)-1] != ';' {
		return s
	}
	if len(s) >= 2 && s[len(s)-2] == '\\' {
		return s // already escaped
	}
	return s[:len(s)-1] + `\;`
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
		if isNoSessionError(stderr) || isNoServerError(stderr) {
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

// statusLineRows maps the value of tmux's "status" option to the number
// of terminal rows the status line occupies at the bottom of a client:
// "off" → 0, "on" → 1, "2".."5" → that many rows. Unknown values fall
// back to 1 (the tmux default is "on").
func statusLineRows(status string) int {
	switch s := strings.TrimSpace(status); s {
	case "off":
		return 0
	case "on", "":
		return 1
	case "2", "3", "4", "5":
		return int(s[0] - '0')
	default:
		return 1
	}
}

// PinWindowSize pins the session's window to exactly the drawable area of
// a clientCols×clientRows client: tmux reserves statusLineRows rows of
// the client for the status line, so the window itself must be that much
// shorter or tmux vertically pans the view (visible as a "[0,N]"
// indicator and misplaced redraws). `resize-window -x -y` both resizes
// the window and flips its window-size option to "manual", which is
// exactly the pin semantic: the window stops following other clients
// until UnpinWindowSize.
func (t *tmuxRuntime) PinWindowSize(ctx context.Context, h Handle, clientCols, clientRows int) error {
	if err := validateName(h.Name); err != nil {
		return err
	}
	status, _, err := runTmux(ctx, "display-message", "-p", "-t", paneTarget(h.Name), "#{status}")
	if err != nil {
		return fmt.Errorf("query status option for %q: %w", h.Name, err)
	}
	rows := clientRows - statusLineRows(status)
	if rows < 1 {
		rows = 1
	}
	if clientCols < 1 {
		clientCols = 1
	}
	if _, _, err := runTmux(ctx, "resize-window", "-t", paneTarget(h.Name),
		"-x", fmt.Sprintf("%d", clientCols), "-y", fmt.Sprintf("%d", rows)); err != nil {
		return fmt.Errorf("pin window size for %q: %w", h.Name, err)
	}
	return nil
}

// UnpinWindowSize undoes PinWindowSize: it overwrites the "manual"
// window-size (set implicitly by resize-window) with the base "latest"
// policy at window scope, so the window follows attached clients again.
// Window scope is used deliberately — it is exactly where resize-window
// wrote "manual", and a window-scope value always wins, so this restores
// automatic sizing no matter what other scopes hold. Safe to call on a
// never-pinned session.
func (t *tmuxRuntime) UnpinWindowSize(ctx context.Context, h Handle) error {
	if err := validateName(h.Name); err != nil {
		return err
	}
	if _, _, err := runTmux(ctx, "set-option", "-w", "-t", paneTarget(h.Name), "window-size", "latest"); err != nil {
		return fmt.Errorf("restore window-size for %q: %w", h.Name, err)
	}
	return nil
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

// LooksLikeQuizWidget reports whether a pane tail is showing Claude Code's
// interactive AskUserQuestion widget. Matched against the two stable
// markers observed live (docs/superpowers/recon/2026-07-19-quiz-recon.md
// §2 + live acceptance, CLI v2.1.215): the footer hint line («Enter to select · …», present
// on every widget screen) and the tab row's «✔ Submit» caption (present on
// multi-question quizzes and the review screen). This is deliberately
// narrow, Claude-specific TUI coupling: generic "pane changed" heuristics
// cannot be used to stop Enter retries because Claude Code's spinner
// animates the tail even while a draft is still unsubmitted.
func LooksLikeQuizWidget(tail string) bool {
	return strings.Contains(tail, "Enter to select · ") ||
		strings.Contains(tail, "✔ Submit") ||
		strings.Contains(tail, "Ready to submit your answers?")
}

// inputWaitCaptions are prompt captions Claude Code (and ordinary CLI tools
// run inside the pane) print when they are blocked on a human answer.
var inputWaitCaptions = []string{
	"Do you want to",    // tool-permission prompts ("Do you want to proceed?")
	"Would you like to", // plan-mode approval
	"Do you trust",      // first-run folder-trust prompt
	"(y/n)", "(Y/n)", "(y/N)", "[y/n]", "[Y/n]", "[y/N]",
}

// selectionCursorRe matches the highlighted row of a numbered selection list
// ("❯ 1. Yes"); selectionOptionRe matches a non-highlighted option row
// ("  2. No"). Both are required — see LooksLikeInputWait.
var (
	selectionCursorRe = regexp.MustCompile(`(?m)^\s*❯\s*\d+[.)]\s`)
	selectionOptionRe = regexp.MustCompile(`(?m)^\s{2,}\d+[.)]\s`)
)

// LooksLikeInputWait reports whether a pane tail is showing something that
// is actually blocked on a human answer: Claude Code's AskUserQuestion
// widget, a tool-permission / approval prompt, or a plain CLI yes-no
// question.
//
// It exists because the waiting_input activity state is set by Claude
// Code's Notification hook, which also fires when the agent has merely been
// idle for a while — so a live session would keep claiming it waits on the
// human forever (see monitor.pollSession's stale-waiting_input correction).
// The monitor only uses a NEGATIVE answer here, and only to demote
// waiting_input back to ready, so the checks lean deliberately generous:
// a false positive costs nothing but one more poll interval of a stale
// state, while a false negative would hide a real prompt from the human.
//
// The numbered-list check requires both the highlighted cursor row and at
// least one further option row, so a draft the human typed into the
// composer ("❯ 1. посмотри код") is not mistaken for a prompt.
func LooksLikeInputWait(pane string) bool {
	if LooksLikeQuizWidget(pane) {
		return true
	}
	for _, caption := range inputWaitCaptions {
		if strings.Contains(pane, caption) {
			return true
		}
	}
	return selectionCursorRe.MatchString(pane) && selectionOptionRe.MatchString(pane)
}
