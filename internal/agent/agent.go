package agent

import (
	"context"
	"errors"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
)

// ErrNoSignal indicates the agent adapter could not find any activity
// signal (e.g. no transcript file) for the given reference.
var ErrNoSignal = errors.New("no activity signal")

// ActivityRef identifies the agent session/worktree to inspect for
// activity state.
type ActivityRef struct {
	SessionID    string
	WorktreePath string
}

// ChatEntry is one entry in a session's chat transcript, as returned by
// Agent.TranscriptTail.
type ChatEntry struct {
	Role     string // "user" | "assistant" | "tool"
	Text     string // message text; for Role=="tool", a short digest of the tool call
	ToolName string // set only when Role=="tool" (e.g. "Bash", "Edit")
	TS       int64  // unix seconds from the transcript record; 0 if absent
}

// LaunchSpec contains all configuration needed to launch an agent.
type LaunchSpec struct {
	SessionID      string
	Kind           string
	ParentID       string
	ProjectID      string
	RepoID         string
	Feature        string
	WorktreePath   string
	SystemPrompt   string
	FirstMessage   string
	Model          string
	PermissionMode string
	SocketPath     string
}

// Agent represents an AI coding agent that can be launched with a given spec.
type Agent interface {
	// Name returns the name of the agent (e.g., "claude-code").
	Name() string

	// Available checks if the agent is available for use (e.g., executable in PATH).
	Available() error

	// SetupWorkspace prepares the workspace for agent launch.
	// In phase 1, this writes the system prompt to a file if provided.
	SetupWorkspace(spec LaunchSpec) error

	// LaunchCommand returns the command and arguments needed to launch the agent.
	LaunchCommand(spec LaunchSpec) []string

	// Env returns environment variables needed for the agent.
	Env(spec LaunchSpec) map[string]string

	// Activity returns the raw activity state and last-work timestamp for
	// the given agent session, based on source signals (e.g. transcript
	// files). Thresholds (e.g. downgrading Ready to Idle) are applied by
	// the monitor, not here. Returns ErrNoSignal if no signal is found.
	Activity(ctx context.Context, ref ActivityRef) (activity.State, time.Time, error)

	// TranscriptTail returns chat entries recorded after cursor (""=from
	// the start of the current transcript) and the new cursor to resume
	// from on the next call. The cursor is opaque to callers: store and
	// return it verbatim. Returns ErrNoSignal if no transcript is found.
	TranscriptTail(ctx context.Context, ref ActivityRef, cursor string) ([]ChatEntry, string, error)

	// TranscriptStat returns the modification time (unix seconds) and size
	// (bytes) of the current transcript file, for cheap polling of whether
	// the chat has changed. Returns ErrNoSignal if no transcript is found.
	TranscriptStat(ctx context.Context, ref ActivityRef) (mtime int64, size int64, err error)
}

// builders holds agent constructor functions, populated by agent implementations via Register.
var builders = map[string]func() Agent{}

// Register registers an agent builder under the given name.
func Register(name string, builder func() Agent) {
	builders[name] = builder
}
