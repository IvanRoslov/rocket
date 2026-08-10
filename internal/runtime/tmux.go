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
	"unicode"
)

// ErrSubmitUnconfirmed is returned by Inject when the text was pasted and
// Enter was sent the configured number of times, submission could not be
// confirmed via the poll predicate before attempts were exhausted, AND the
// final whole-pane check could not settle the question either (the capture
// itself failed). This is the genuinely unknown outcome: the text may well
// have been submitted, and it may still be sitting in the composer.
// Callers MUST NOT blindly re-inject on this error, since the original
// text may already have reached the target program: re-injecting risks
// sending a duplicate message. Callers should typically surface this to
// the operator or fall back to an out-of-band check (e.g. Capture) before
// deciding whether to retry.
//
// For the case where Inject positively established that nothing was
// submitted, see ErrNotDelivered.
var ErrSubmitUnconfirmed = errors.New("inject: submission unconfirmed after exhausting attempts")

// ErrNotDelivered is returned by Inject when the attempts were exhausted
// and the final whole-pane check found no trace of the text in the pane's
// history: nothing was submitted. Inject has cleared the composer and
// dropped the paste buffer, so no orphaned draft is left behind and the
// text is definitively gone. Unlike ErrSubmitUnconfirmed, this carries no
// duplicate-message risk: callers may re-inject freely, and MUST NOT
// record the message as delivered.
var ErrNotDelivered = errors.New("inject: text was not delivered; composer cleared")

// ErrComposerBusy is returned by Inject when the recipient's composer holds
// what looks like an unsent draft a human is typing (see
// LooksLikeUserDraft). Nothing was cleared, pasted or submitted: the guard
// runs before the pre-paste C-u precisely so that the human's text survives.
// Unlike ErrNotDelivered this is not a failed delivery attempt but a
// deferral — the caller is expected to hold the message and try again
// later, without burning a retry attempt, and to eventually fall back to
// InjectOpts{Force: true} so an abandoned draft cannot block a queue
// forever.
var ErrComposerBusy = errors.New("inject: composer holds a user draft; nothing was cleared or sent")

// ErrHeldDialog reports that the recipient's pane is showing Claude Code's
// held-peer-message approval dialog, so nothing was cleared or sent. Like
// ErrComposerBusy this is a deferral, not a failure: the caller must put the
// message back and try again once the dialog is gone.
var ErrHeldDialog = errors.New("inject: recipient is showing a held-message dialog; nothing was cleared or sent")

// nameRE is the set of characters permitted in a session name. It is
// deliberately restrictive: session names are interpolated into tmux
// targets as "=name", and tmux target syntax treats many characters
// (":", ".", "%", etc.) specially.
var nameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// defaultPollInterval and defaultPollTimeout are Inject's production
// confirmation-polling timings (see tmuxRuntime.pollInterval).
const (
	defaultPollInterval = 300 * time.Millisecond
	defaultPollTimeout  = 1500 * time.Millisecond
)

// finalCheckScrollback is how many lines of scrollback Inject's final
// delivered-or-stuck check looks back through. It has to cover whatever the
// agent rendered between the submit and that check — a few seconds of an
// agent's turn, which can be long — while staying far short of the pane's
// full history, which would make every failed injection capture megabytes.
const finalCheckScrollback = 500

// tmuxRuntime is the tmux-backed implementation of Runtime.
type tmuxRuntime struct {
	// settleFn is called with a duration to pause before the first Enter
	// attempt on a large paste (see the settle-pause comment in Inject).
	// Defaults to time.Sleep; overridable by tests to observe the call
	// without actually waiting.
	settleFn func(time.Duration)

	// runFn runs one tmux command. Defaults (when nil) to the real
	// runTmux; tests substitute a fake to drive Inject's confirmation
	// logic deterministically and to assert on the exact command
	// sequence.
	runFn func(ctx context.Context, args ...string) (stdout, stderr string, err error)

	// pollInterval and pollTimeout bound Inject's confirmation polling.
	// Zero means "use the defaults" (defaultPollInterval /
	// defaultPollTimeout); tests shrink them to keep an
	// exhausted-attempts run fast.
	pollInterval time.Duration
	pollTimeout  time.Duration
}

// run dispatches a tmux command through runFn, falling back to the real
// tmux binary when no override is installed.
func (t *tmuxRuntime) run(ctx context.Context, args ...string) (string, string, error) {
	if t.runFn != nil {
		return t.runFn(ctx, args...)
	}
	return runTmux(ctx, args...)
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
func (t *tmuxRuntime) Inject(ctx context.Context, h Handle, text string, opts InjectOpts) error {
	if err := validateName(h.Name); err != nil {
		return err
	}

	// 0. Draft guard. The C-u below is indiscriminate: it erases whatever
	// sits on the input line, including a message a human is halfway
	// through typing (which is exactly what it looked like to the person
	// who lost their text). Check for that immediately before clearing —
	// not in the caller — so the window between "looked free" and "cleared"
	// stays as small as the two tmux calls that bracket it.
	//
	// Fail-open by design: a capture error, or any composer rendering
	// LooksLikeUserDraft does not positively recognise, proceeds exactly
	// as before the guard existed. The cost of a wrong "busy" is a delayed
	// message (recoverable: the queue retries, and its busy-deadline forces
	// delivery through), but the cost of a wrong "free" is destroyed human
	// input — so the uncertain case must never be the destructive one.
	//
	// The same capture also feeds the held-dialog guard below, which — unlike
	// the draft guard — is NOT subject to opts.Force, so the capture happens
	// unconditionally.
	//
	// -e keeps the pane's escape sequences: the dim attribute is the only
	// thing that tells a typed draft from Claude Code's ghost-text
	// autosuggestion, which renders identically without it.
	if out, _, err := t.run(ctx, "capture-pane", "-p", "-e", "-t", paneTarget(h.Name)); err == nil {
		// 0a. Held-dialog guard. While Claude Code's held-peer-message
		// approval dialog is on screen, keystrokes drive its selector rather
		// than the composer: the paste+Enter below would dismiss the dialog
		// (dropping the held copy) AND never reach the conversation — both
		// copies of the message lost. Force does not bypass this: it is the
		// escape hatch for an abandoned draft, whose loss is recoverable,
		// whereas this one never is.
		if LooksLikeHeldPeerDialog(out) {
			return fmt.Errorf("%w: session %q", ErrHeldDialog, h.Name)
		}
		if !opts.Force && LooksLikeUserDraft(out) {
			return fmt.Errorf("%w: session %q", ErrComposerBusy, h.Name)
		}
	}

	// 1. Clear any existing draft on the input line. send-keys and
	// paste-buffer address a pane, not a session, so they need the
	// colon-suffixed pane target even though has-session/kill-session
	// accept the bare session target.
	if _, _, err := t.run(ctx, "send-keys", "-t", paneTarget(h.Name), "C-u"); err != nil {
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
	if _, _, err := t.run(ctx, "load-buffer", "-b", bufName, tmpPath); err != nil {
		return fmt.Errorf("load buffer: %w", err)
	}
	if _, _, err := t.run(ctx, "paste-buffer", "-d", "-b", bufName, "-t", paneTarget(h.Name)); err != nil {
		return fmt.Errorf("paste buffer: %w", err)
	}

	// 3. Adaptive submit: press Enter, then poll until the draft is gone
	// from the visible tail (i.e. it was actually submitted). Some
	// programs / TUIs occasionally swallow the first Enter, so retry.
	if strings.TrimRight(text, "\n") == "" {
		// Nothing meaningful to verify; a single Enter is sufficient.
		_, _, err := t.run(ctx, "send-keys", "-t", paneTarget(h.Name), "Enter")
		return err
	}

	const maxAttempts = 5

	pollInterval := t.pollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	pollTimeout := t.pollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultPollTimeout
	}

	// marker is a *logical* line; the pane may hold it broken across rows.
	// Every comparison against it therefore goes through ContainsMarker,
	// never strings.Contains — see its doc for why a raw match silently
	// turns every long message into a redelivery storm.
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
			out, _, err := t.run(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
			if err == nil && ContainsMarker(tailLines(trimTrailingBlank(out), confirmWindow), marker) {
				markerSeen = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(pollInterval)
		}
	}

	baseline, _, err := t.run(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
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
			if out, _, err := t.run(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name)); err == nil {
				if LooksLikeQuizWidget(tailLines(trimTrailingBlank(out), confirmWindow)) {
					return nil
				}
			}
		}
		if _, _, err := t.run(ctx, "send-keys", "-t", paneTarget(h.Name), "Enter"); err != nil {
			return fmt.Errorf("send Enter: %w", err)
		}

		deadline := time.Now().Add(pollTimeout)
		for {
			full, _, err := t.run(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name))
			if err != nil {
				return fmt.Errorf("poll capture-pane: %w", err)
			}
			out := tailLines(trimTrailingBlank(full), confirmWindow)
			// marker-absent: the injected text's last line no longer
			// appears anywhere in the tail — the input box was cleared.
			// Handles full-screen/alt-screen TUIs that redraw with a
			// static line count and footer on submit. Only valid if
			// markerSeen is true (marker was rendered in the baseline).
			markerAbsent := markerSeen && marker != "" && !ContainsMarker(out, marker)
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

	// 4. Attempts exhausted. The text is now in one of two very different
	// states, and they must not be conflated: either it WAS submitted and
	// only the confirmation was missed (its echo sits in the pane's
	// history area above the composer), or it is still sitting in the
	// composer as an unsent draft — orphaned text that is
	// indistinguishable from something a human typed and one stray Enter
	// away from being sent (live incident, task #1050).
	//
	// A final whole-pane capture tells them apart, looking for the marker
	// everywhere ABOVE the confirmWindow tail. The tail is excluded on
	// purpose: it is the composer, and the marker is necessarily still
	// there in the stuck case (had it left, marker-absent would already
	// have confirmed) — so including it would find the marker either way
	// and decide nothing. The history area above it, by contrast, only
	// holds the marker once the message was actually submitted.
	// This capture — unlike the polling ones above, which examine the
	// composer at the bottom of the visible screen — must include the
	// scrollback (-S). A busy agent starts rendering its turn the instant
	// the message is submitted, so within the few seconds the retry loop
	// takes the message is pushed off the visible screen entirely. Judging
	// by the visible screen alone, a delivered message is indistinguishable
	// from a stuck draft: Inject reports a non-delivery, the queue retries,
	// and the agent gets the same message several times (live incidents
	// #1186/Q5 and papercuts-sdk-app-21, where the marker was provably in
	// the pane's scrollback and absent from the visible screen).
	if marker != "" {
		full, _, err := t.run(ctx, "capture-pane", "-p", "-t", paneTarget(h.Name),
			"-S", fmt.Sprintf("-%d", finalCheckScrollback))
		if err == nil {
			// StripHeldPeerBanner first: a session that ever held one of our
			// messages keeps a banner quoting that message's text back in its
			// «preview» forever. Left in, it makes this check confirm a
			// message that provably never reached the conversation — the
			// exact non-delivery the held-dialog blocker exists to prevent.
			if ContainsMarker(StripHeldPeerBanner(history(trimTrailingBlank(full), confirmWindow)), marker) {
				// Delivered after all — never wipe a composer whose
				// content actually went through.
				return nil
			}
			// Marker nowhere in history: the draft really is stuck.
			// Leave no orphan behind — C-u clears the composer's input
			// line and kill-buffer drops the paste buffer so a stray
			// paste cannot resurrect the text. Both are best-effort; the
			// honest non-delivery is what the caller must see.
			_, _, _ = t.run(ctx, "send-keys", "-t", paneTarget(h.Name), "C-u")
			_, _, _ = t.run(ctx, "kill-buffer", "-b", bufName)
			return fmt.Errorf("%w: after %d attempts", ErrNotDelivered, maxAttempts)
		}
		// The final capture itself failed, so delivery could not be
		// established either way. Deliberately: classify as unknown
		// (ErrSubmitUnconfirmed, not ErrNotDelivered) — a flaky capture
		// must not turn a delivered message into a retry storm — and
		// clear nothing. Clearing would be defensible (C-u on an
		// already-empty composer is harmless), but it buys nothing here:
		// the caller is told "unknown" either way and will not treat this
		// as a non-delivery, while a blind C-u on an unreadable pane is
		// the one case where we cannot see what we are wiping.
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
// ContainsMarker reports whether haystack (a pane capture) shows marker (a
// logical line of injected text), ignoring how the pane broke that line
// across rows.
//
// A plain strings.Contains cannot answer this. The marker is one logical
// line, but nothing guarantees the pane holds it as one row:
//
//   - tmux wraps any line longer than the pane is wide, so capture-pane
//     reports it as several rows (`capture-pane -J` rejoins exactly these,
//     and only these);
//   - chat-style TUIs (Claude Code) do their own wrapping — they re-flow the
//     message to their frame, prefix the first row ("❯ "), indent the rest,
//     and pad every row to the full width. Those are real newlines printed
//     by the application, so -J leaves them split.
//
// The second case is the one that bit us: the marker was findable nowhere,
// submission could never be confirmed, Inject reported a non-delivery for
// text that had in fact landed, and the queue redelivered it up to
// maxAttempts (live incident: a #1186/Q5 message pasted into cto five
// times, the agent itself replying "Дубль, не реагирую").
//
// Comparing with all whitespace removed is insensitive to both: wrapping
// only ever inserts breaks, indentation and padding — never non-space
// characters — so a marker that is present survives normalisation, while
// the message text itself stays distinctive enough not to collide.
func ContainsMarker(haystack, marker string) bool {
	if strings.TrimSpace(marker) == "" {
		return false
	}
	return strings.Contains(stripWhitespace(haystack), stripWhitespace(marker))
}

// stripWhitespace removes every whitespace character, collapsing wrapped,
// indented and padded renderings of the same text to one comparable form.
func stripWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

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

// history returns everything in s except its last n lines — i.e. the pane
// above the confirmation window. It is the complement of tailLines: where
// tailLines isolates the composer/footer chrome, history isolates the
// scrolling area a submitted message is echoed into.
func history(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return ""
	}
	return strings.Join(lines[:len(lines)-n], "\n")
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

// CaptureEscaped is Capture with `-e`, so the pane comes back with its ANSI
// escape sequences intact. Blank-padding is trimmed on each row's VISIBLE
// text: with escapes kept, an unwritten row still carries the sequences tmux
// re-emits per line, and a byte-level blankness check would count it as
// content and push the composer out of the window.
func (t *tmuxRuntime) CaptureEscaped(ctx context.Context, h Handle, lines int) (string, error) {
	if err := validateName(h.Name); err != nil {
		return "", err
	}
	out, _, err := runTmux(ctx, "capture-pane", "-p", "-e", "-t", paneTarget(h.Name), "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", fmt.Errorf("capture pane %q: %w", h.Name, err)
	}
	return tailVisibleLines(out, lines), nil
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
