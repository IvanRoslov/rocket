// Package runtime abstracts the terminal multiplexer that rocket uses to
// run AI agent sessions. The current (and only) implementation is tmux.
package runtime

import "context"

// Handle identifies a running session by name.
type Handle struct {
	Name string
}

// CreateSpec describes a session to create.
type CreateSpec struct {
	Name    string
	Dir     string
	Command string
	Env     map[string]string
}

// Runtime manages agent sessions running inside a terminal multiplexer.
//
// These signatures are load-bearing: the session manager and phase 2
// depend on this exact interface. Do not change without updating callers.
type Runtime interface {
	// Create starts a new session running spec.Command in spec.Dir with
	// spec.Env set, and returns a Handle referring to it.
	Create(ctx context.Context, spec CreateSpec) (Handle, error)
	// Inject types text into the session's active pane and submits it.
	// Beyond nil (submitted) it reports two distinct failure outcomes:
	// ErrNotDelivered — nothing was submitted and the composer has been
	// cleared, so re-injecting is safe and the message must not be
	// recorded as delivered; and ErrSubmitUnconfirmed — delivery could
	// not be established either way, where re-injecting risks a duplicate.
	Inject(ctx context.Context, h Handle, text string) error
	// SendKeys sends a single logical key to the session's active pane via
	// tmux send-keys: either a key name tmux recognizes (e.g. "Enter",
	// "Tab", "Down", "Space", a bare digit) when literal is false, or raw
	// literal text (sent with tmux's -l flag, bypassing key-name lookup so
	// e.g. punctuation isn't misinterpreted) when literal is true. Unlike
	// Inject, SendKeys does not clear any draft first and does not attempt
	// to confirm delivery — it is a single low-level keystroke, for callers
	// (the quiz-answer keystroke injector in internal/session) that need to
	// drive a multi-step TUI flow one key at a time rather than submit one
	// block of text.
	SendKeys(ctx context.Context, h Handle, key string, literal bool) error
	// Capture returns the last `lines` lines of the session's pane output.
	Capture(ctx context.Context, h Handle, lines int) (string, error)
	// Alive reports whether the session still exists.
	Alive(ctx context.Context, h Handle) bool
	// Destroy terminates the session. Destroying a session that does not
	// exist is not an error.
	Destroy(ctx context.Context, h Handle) error
	// AttachCommand returns the argv a user can run to attach to the
	// session interactively.
	AttachCommand(h Handle) []string
	// PinWindowSize pins the session's window to exactly the area a
	// client of clientCols×clientRows can display (minus multiplexer
	// chrome such as the tmux status line), overriding any automatic
	// window sizing until UnpinWindowSize is called. While pinned, other
	// attached clients see a cropped/padded view; the pinning client
	// renders pixel-perfect. See docs/03-daemon-api.md «Размер окна».
	PinWindowSize(ctx context.Context, h Handle, clientCols, clientRows int) error
	// UnpinWindowSize removes a PinWindowSize override and restores
	// automatic window sizing driven by attached clients. Unpinning a
	// session that was never pinned is not an error.
	UnpinWindowSize(ctx context.Context, h Handle) error
	// List returns the names of all currently live sessions, for
	// reconciliation against rocket's own bookkeeping.
	List(ctx context.Context) ([]string, error)
}
