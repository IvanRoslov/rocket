// Package modelpolicy decides which model profile a launch gets (task #5026,
// choose-model). It is pure: callers load the registry, the feature task's
// allowlist and the default settings, and pass them in. Every launch path —
// worker spawn, task start — resolves through here; restore does not, it
// relaunches from the session's own snapshot.
package modelpolicy

import (
	"sort"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Policy error codes, verbatim from the spec. They travel to API clients as
// the machine-readable error code.
const (
	CodeNotFound    = "profile_not_found"
	CodeNotAllowed  = "profile_not_allowed"
	CodeNoneAllowed = "no_profiles_allowed"
	CodeBadRequest  = "bad_request"
)

// Inputs is everything a decision depends on.
type Inputs struct {
	// Profiles is the full registry, enabled or not, in any order.
	Profiles []store.ModelProfile
	// TaskAllowed is the feature task's worker allowlist; nil or empty means
	// every enabled profile. Names missing from Profiles are ignored.
	TaskAllowed []string
	// DefaultWorker / DefaultOrch are the default_worker_profile /
	// default_orchestrator_profile settings ("" when unset).
	DefaultWorker string
	DefaultOrch   string
	// DefaultAgent is config's default_agent.
	DefaultAgent string
}

// PolicyError is a refused resolution. Allowed lists the profile names the
// caller could have used, when that helps (profile_not_allowed).
type PolicyError struct {
	Code    string
	Allowed []string
	Msg     string
}

// Error renders "code", "code: allowed: a, b" or "code: msg".
func (e *PolicyError) Error() string {
	switch {
	case e.Msg != "":
		return e.Code + ": " + e.Msg
	case len(e.Allowed) > 0:
		return e.Code + ": allowed: " + strings.Join(e.Allowed, ", ")
	default:
		return e.Code
	}
}

// Allowed returns the profiles a worker of the feature task may run with:
// the enabled ones, narrowed to TaskAllowed when it is non-empty, ordered by
// position, name. An allowlist whose names all vanished yields nothing — it
// never widens back to "everything".
func Allowed(in Inputs) []store.ModelProfile {
	var permit map[string]bool
	if len(in.TaskAllowed) > 0 {
		permit = make(map[string]bool, len(in.TaskAllowed))
		for _, n := range in.TaskAllowed {
			permit[n] = true
		}
	}
	out := []store.ModelProfile{}
	for _, p := range sorted(in.Profiles) {
		if p.Enabled && (permit == nil || permit[p.Name]) {
			out = append(out, p)
		}
	}
	return out
}

// DefaultWorkerName is the profile a worker gets when the spawn names
// neither a profile nor an agent; "" when nothing is allowed.
func DefaultWorkerName(in Inputs) string {
	p, err := ResolveWorker(in, "", "")
	if err != nil {
		return ""
	}
	return p.Name
}

// ResolveWorker picks the profile for a worker spawn: an explicit profile
// must be in Allowed; an agent alone takes the first allowed profile of that
// agent; nothing takes DefaultWorker when allowed, else the first allowed.
// A profile and agent that disagree are a bad request.
func ResolveWorker(in Inputs, profile, agent string) (store.ModelProfile, error) {
	allowed := Allowed(in)
	if profile != "" {
		p, ok := find(in.Profiles, profile)
		if !ok {
			return store.ModelProfile{}, &PolicyError{Code: CodeNotFound, Msg: "no profile " + profile}
		}
		if agent != "" && p.Agent != agent {
			return store.ModelProfile{}, agentMismatch(p, agent)
		}
		if len(allowed) == 0 {
			return store.ModelProfile{}, &PolicyError{Code: CodeNoneAllowed}
		}
		if _, ok := find(allowed, profile); !ok {
			return store.ModelProfile{}, &PolicyError{Code: CodeNotAllowed, Allowed: namesOf(allowed)}
		}
		return p, nil
	}
	if len(allowed) == 0 {
		return store.ModelProfile{}, &PolicyError{Code: CodeNoneAllowed}
	}
	if agent != "" {
		for _, p := range allowed {
			if p.Agent == agent {
				return p, nil
			}
		}
		return store.ModelProfile{}, &PolicyError{Code: CodeNotAllowed, Allowed: namesOf(allowed)}
	}
	if p, ok := find(allowed, in.DefaultWorker); ok {
		return p, nil
	}
	return allowed[0], nil
}

// ResolveOrchestrator picks the profile for a task's orchestrator. The task
// allowlist does not apply — the human picks the orchestrator — only the
// enabled flag does. ok=false means "launch the legacy way, without a
// profile": no profile fits and that must not break starting a task.
func ResolveOrchestrator(in Inputs, profile, agent string) (store.ModelProfile, bool, error) {
	enabled := Allowed(Inputs{Profiles: in.Profiles})
	if profile != "" {
		p, ok := find(in.Profiles, profile)
		if !ok {
			return store.ModelProfile{}, false, &PolicyError{Code: CodeNotFound, Msg: "no profile " + profile}
		}
		if agent != "" && p.Agent != agent {
			return store.ModelProfile{}, false, agentMismatch(p, agent)
		}
		if !p.Enabled {
			return store.ModelProfile{}, false, &PolicyError{Code: CodeNotAllowed, Allowed: namesOf(enabled)}
		}
		return p, true, nil
	}
	if agent != "" {
		for _, p := range enabled {
			if p.Agent == agent {
				return p, true, nil
			}
		}
		return store.ModelProfile{}, false, nil
	}
	if p, ok := find(enabled, in.DefaultOrch); ok {
		return p, true, nil
	}
	for _, p := range enabled {
		if p.Agent == in.DefaultAgent {
			return p, true, nil
		}
	}
	return store.ModelProfile{}, false, nil
}

func agentMismatch(p store.ModelProfile, agent string) error {
	return &PolicyError{Code: CodeBadRequest,
		Msg: "profile " + p.Name + " runs agent " + p.Agent + ", not " + agent}
}

func sorted(ps []store.ModelProfile) []store.ModelProfile {
	out := append([]store.ModelProfile(nil), ps...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func find(ps []store.ModelProfile, name string) (store.ModelProfile, bool) {
	if name == "" {
		return store.ModelProfile{}, false
	}
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return store.ModelProfile{}, false
}

func namesOf(ps []store.ModelProfile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
