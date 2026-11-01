// Package claudeinstance binds Claude socket operations to an exact Rocket launch.
package claudeinstance

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Launch struct {
	BindingRevision                           uint64
	RocketSessionID, Generation, PaneIdentity string
	PanePID                                   int
	PaneStart                                 time.Time
	StartedAt                                 time.Time
}
type Identity struct {
	RocketSessionID, LaunchGeneration, ClaudeSessionID string
	PID                                                int
	ProcessStart                                       time.Time
	PaneIdentity                                       string
	PeerProtocol                                       int
	Kind, SocketPath, JournalPath                      string
}
type Liveness string

const (
	Verified Liveness = "verified"
	Dead     Liveness = "dead"
	Unknown  Liveness = "unknown"
	Rejected Liveness = "rejected"
)

type Resolution struct {
	Identity Identity
	State    Liveness
	Reason   string
}
type Process struct {
	PID, PPID int
	Start     time.Time
	Name      string
}
type Pane struct {
	Identity string
	PID      int
	Start    time.Time
}
type Probe interface {
	Process(context.Context, int) (Process, bool, error)
	Pane(context.Context, string) (Pane, bool, error)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var projectSlug = strings.NewReplacer("/", "-", ".", "-")

func Resolve(ctx context.Context, launch Launch, pinned *Identity, registry []socketmsg.Session, probe Probe, projectsDir string) Resolution {
	result := func(state Liveness, reason string) Resolution { return Resolution{State: state, Reason: reason} }
	if launch.RocketSessionID == "" || launch.Generation == "" || launch.PanePID <= 0 || launch.PaneIdentity == "" || launch.PaneStart.IsZero() || launch.StartedAt.IsZero() {
		return result(Rejected, "unproven_launch")
	}
	if pinned != nil && (pinned.RocketSessionID != launch.RocketSessionID || pinned.LaunchGeneration != launch.Generation || pinned.PaneIdentity != launch.PaneIdentity) {
		return result(Rejected, "launch_mismatch")
	}
	if probe == nil {
		return result(Unknown, "probe_unavailable")
	}
	// Only a previously bound exact process can establish death. Registry absence
	// or command failure alone cannot distinguish unavailable evidence from death.
	if pinned != nil {
		p, exists, err := probe.Process(ctx, pinned.PID)
		if err != nil {
			return result(Unknown, "process_lookup")
		}
		if !exists || !p.Start.Equal(pinned.ProcessStart) {
			return result(Dead, "exact_root_gone")
		}
	}
	pane, exists, err := probe.Pane(ctx, launch.RocketSessionID)
	if err != nil {
		return result(Unknown, "pane_lookup")
	}
	if !exists {
		return result(Dead, "runtime_gone")
	}
	if pane.Identity != launch.PaneIdentity || pane.PID != launch.PanePID || !pane.Start.Equal(launch.PaneStart) {
		return result(Rejected, "pane_mismatch")
	}
	var candidates []socketmsg.Session
	for _, r := range registry {
		name, _, _ := strings.Cut(r.Tmux, ":")
		if name == launch.RocketSessionID {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) != 1 {
		return result(Rejected, "ambiguous_or_missing_registry")
	}
	r := candidates[0]
	for _, other := range registry {
		if other.SessionID == r.SessionID && (other.PID != r.PID || other.Tmux != r.Tmux) {
			return result(Rejected, "ambiguous_uuid_writer")
		}
	}
	if r.Tmux != pane.Identity || r.Kind != "interactive" || r.PeerProtocol != 1 || r.MessagingSocketPath == "" || !uuidPattern.MatchString(r.SessionID) || !filepath.IsAbs(r.CWD) || !filepath.IsAbs(r.MessagingSocketPath) {
		return result(Rejected, "registry_identity")
	}
	start, err := ParseProcessStart(r.ProcStart)
	if err != nil || start.Before(launch.StartedAt.Truncate(time.Second)) {
		return result(Rejected, "process_start")
	}
	process, exists, err := probe.Process(ctx, r.PID)
	if err != nil {
		return result(Unknown, "process_lookup")
	}
	if !exists || !process.Start.Equal(start) {
		return result(Rejected, "registry_process_mismatch")
	}
	if filepath.Base(process.Name) != "claude" {
		return result(Rejected, "not_claude_root")
	}
	seen := map[int]bool{r.PID: true}
	parent := process
	// The pane's shell may exec the root, but a second Claude below a root is
	// never the target. Every parent lookup must succeed; cap corrupt cycles.
	for parent.PID != launch.PanePID {
		if parent.PPID <= 1 || seen[parent.PPID] || len(seen) > 64 {
			return result(Rejected, "outside_launch_pane")
		}
		seen[parent.PPID] = true
		parent, exists, err = probe.Process(ctx, parent.PPID)
		if err != nil {
			return result(Unknown, "ancestry_lookup")
		}
		if !exists {
			return result(Rejected, "ancestry_gone")
		}
		if filepath.Base(parent.Name) == "claude" {
			return result(Rejected, "nested_root")
		}
	}
	if !parent.Start.Equal(launch.PaneStart) {
		return result(Rejected, "pane_pid_reused")
	}
	cwd := r.CWD
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	id := Identity{RocketSessionID: launch.RocketSessionID, LaunchGeneration: launch.Generation, ClaudeSessionID: r.SessionID, PID: r.PID, ProcessStart: start.UTC(), PaneIdentity: r.Tmux, PeerProtocol: r.PeerProtocol, Kind: r.Kind, SocketPath: r.MessagingSocketPath, JournalPath: filepath.Join(projectsDir, projectSlug.Replace(cwd), r.SessionID+".jsonl")}
	if pinned != nil && !SameIdentity(*pinned, id) {
		prior := *pinned
		prior.ClaudeSessionID = id.ClaudeSessionID
		prior.JournalPath = id.JournalPath
		if !uuidPattern.MatchString(pinned.ClaudeSessionID) || !SameIdentity(prior, id) || filepath.Dir(pinned.JournalPath) != filepath.Dir(id.JournalPath) {
			return result(Rejected, "binding_changed")
		}
		if _, e := PinJournal(id); e != nil {
			return result(Rejected, "binding_changed")
		}
	}
	return Resolution{Identity: id, State: Verified}
}

func SameIdentity(a, b Identity) bool {
	return a.RocketSessionID == b.RocketSessionID && a.LaunchGeneration == b.LaunchGeneration && a.ClaudeSessionID == b.ClaudeSessionID && a.PID == b.PID && a.ProcessStart.Equal(b.ProcessStart) && a.PaneIdentity == b.PaneIdentity && a.PeerProtocol == b.PeerProtocol && a.Kind == b.Kind && a.SocketPath == b.SocketPath && a.JournalPath == b.JournalPath
}

// ParseProcessStart accepts explicit timezone values and ps output collected
// with TZ=UTC. It never compares locale-specific timestamp strings.
func ParseProcessStart(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "Mon Jan 2 15:04:05 2006", "Mon Jan 2 15:04:05 MST 2006"} {
		if t, e := time.Parse(layout, strings.Join(strings.Fields(raw), " ")); e == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, &time.ParseError{Layout: time.RFC3339Nano, Value: raw}
}
