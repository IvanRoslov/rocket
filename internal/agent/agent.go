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
}

// builders holds agent constructor functions, populated by agent implementations via Register.
var builders = map[string]func() Agent{}

// Register registers an agent builder under the given name.
func Register(name string, builder func() Agent) {
	builders[name] = builder
}
