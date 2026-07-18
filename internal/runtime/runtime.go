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
	Inject(ctx context.Context, h Handle, text string) error
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
	// List returns the names of all currently live sessions, for
	// reconciliation against rocket's own bookkeeping.
	List(ctx context.Context) ([]string, error)
}
