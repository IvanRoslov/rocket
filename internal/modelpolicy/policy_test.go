package modelpolicy

import (
	"errors"
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// registry: opus(cc,0) sonnet(cc,1) codex(codex,2) off(cc,3,disabled) gpt(codex,4)
func registry() []store.ModelProfile {
	return []store.ModelProfile{
		{Name: "codex", Agent: "codex", Enabled: true, Position: 2},
		{Name: "sonnet", Agent: "claude-code", Model: "sonnet", Enabled: true, Position: 1},
		{Name: "off", Agent: "claude-code", Enabled: false, Position: 3},
		{Name: "opus", Agent: "claude-code", Model: "opus", Enabled: true, Position: 0},
		{Name: "gpt", Agent: "codex", Model: "gpt-5", Enabled: true, Position: 4},
	}
}

func names(ps []store.ModelProfile) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestAllowed(t *testing.T) {
	cases := []struct {
		name string
		in   Inputs
		want []string
	}{
		{"no allowlist = all enabled, ordered", Inputs{Profiles: registry()},
			[]string{"opus", "sonnet", "codex", "gpt"}},
		{"empty allowlist = all enabled", Inputs{Profiles: registry(), TaskAllowed: []string{}},
			[]string{"opus", "sonnet", "codex", "gpt"}},
		{"allowlist intersects, keeps registry order", Inputs{Profiles: registry(), TaskAllowed: []string{"gpt", "sonnet"}},
			[]string{"sonnet", "gpt"}},
		{"disabled profile in allowlist is dropped", Inputs{Profiles: registry(), TaskAllowed: []string{"off", "codex"}},
			[]string{"codex"}},
		// Review Focus 5: allowlist naming only deleted profiles empties the set.
		{"deleted names only = nothing", Inputs{Profiles: registry(), TaskAllowed: []string{"gone"}},
			[]string{}},
		{"same position sorts by name", Inputs{Profiles: []store.ModelProfile{
			{Name: "b", Agent: "codex", Enabled: true}, {Name: "a", Agent: "codex", Enabled: true}}},
			[]string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := names(Allowed(tc.in)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveWorker(t *testing.T) {
	cases := []struct {
		name           string
		in             Inputs
		profile, agent string
		want           string
		code           string
		allowed        []string
	}{
		{"explicit allowed", Inputs{Profiles: registry()}, "gpt", "", "gpt", "", nil},
		{"explicit not in allowlist", Inputs{Profiles: registry(), TaskAllowed: []string{"sonnet", "codex"}},
			"opus", "", "", CodeNotAllowed, []string{"sonnet", "codex"}},
		{"explicit disabled", Inputs{Profiles: registry()}, "off", "", "", CodeNotAllowed,
			[]string{"opus", "sonnet", "codex", "gpt"}},
		{"explicit missing", Inputs{Profiles: registry()}, "nope", "", "", CodeNotFound, nil},
		{"explicit with matching agent", Inputs{Profiles: registry()}, "gpt", "codex", "gpt", "", nil},
		{"explicit with other agent", Inputs{Profiles: registry()}, "gpt", "claude-code", "", CodeBadRequest, nil},
		{"agent only picks first allowed of agent", Inputs{Profiles: registry()}, "", "codex", "codex", "", nil},
		{"agent only respects allowlist", Inputs{Profiles: registry(), TaskAllowed: []string{"gpt", "opus"}},
			"", "codex", "gpt", "", nil},
		{"agent only with none allowed", Inputs{Profiles: registry(), TaskAllowed: []string{"opus"}},
			"", "codex", "", CodeNotAllowed, []string{"opus"}},
		{"nothing picks default worker", Inputs{Profiles: registry(), DefaultWorker: "sonnet"}, "", "", "sonnet", "", nil},
		{"default outside allowlist falls to first", Inputs{Profiles: registry(), DefaultWorker: "sonnet",
			TaskAllowed: []string{"gpt", "codex"}}, "", "", "codex", "", nil},
		{"default disabled falls to first", Inputs{Profiles: registry(), DefaultWorker: "off"}, "", "", "opus", "", nil},
		{"no default falls to first", Inputs{Profiles: registry()}, "", "", "opus", "", nil},
		{"empty registry", Inputs{}, "", "", "", CodeNoneAllowed, nil},
		// Review Focus 5: an allowlist of deleted profiles is "nothing", not "all".
		{"allowlist of deleted profiles", Inputs{Profiles: registry(), TaskAllowed: []string{"gone"}, DefaultWorker: "opus"},
			"", "", "", CodeNoneAllowed, nil},
		{"agent only, allowlist of deleted profiles", Inputs{Profiles: registry(), TaskAllowed: []string{"gone"}},
			"", "codex", "", CodeNoneAllowed, nil},
		{"explicit, allowlist of deleted profiles", Inputs{Profiles: registry(), TaskAllowed: []string{"gone"}},
			"opus", "", "", CodeNoneAllowed, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ResolveWorker(tc.in, tc.profile, tc.agent)
			checkResult(t, p.Name, err, tc.want, tc.code, tc.allowed)
		})
	}
}

func TestResolveOrchestrator(t *testing.T) {
	cases := []struct {
		name           string
		in             Inputs
		profile, agent string
		want           string
		ok             bool
		code           string
	}{
		{"explicit enabled", Inputs{Profiles: registry()}, "gpt", "", "gpt", true, ""},
		{"allowlist does not apply", Inputs{Profiles: registry(), TaskAllowed: []string{"codex"}}, "opus", "", "opus", true, ""},
		{"explicit disabled", Inputs{Profiles: registry()}, "off", "", "", false, CodeNotAllowed},
		{"explicit missing", Inputs{Profiles: registry()}, "nope", "", "", false, CodeNotFound},
		{"explicit agent mismatch", Inputs{Profiles: registry()}, "opus", "codex", "", false, CodeBadRequest},
		{"agent only", Inputs{Profiles: registry()}, "", "codex", "codex", true, ""},
		{"agent only, no profile of agent = legacy", Inputs{Profiles: registry()}, "", "other", "", false, ""},
		{"default orchestrator", Inputs{Profiles: registry(), DefaultOrch: "sonnet"}, "", "", "sonnet", true, ""},
		{"default disabled falls to default agent", Inputs{Profiles: registry(), DefaultOrch: "off", DefaultAgent: "codex"},
			"", "", "codex", true, ""},
		{"default unset falls to default agent", Inputs{Profiles: registry(), DefaultAgent: "claude-code"}, "", "", "opus", true, ""},
		{"default missing falls to default agent", Inputs{Profiles: registry(), DefaultOrch: "gone", DefaultAgent: "codex"},
			"", "", "codex", true, ""},
		{"nothing fits = legacy", Inputs{Profiles: registry(), DefaultAgent: "other"}, "", "", "", false, ""},
		{"empty registry = legacy", Inputs{DefaultOrch: "opus", DefaultAgent: "claude-code"}, "", "", "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok, err := ResolveOrchestrator(tc.in, tc.profile, tc.agent)
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			checkResult(t, p.Name, err, tc.want, tc.code, nil)
		})
	}
}

func TestDefaultWorkerName(t *testing.T) {
	if got := DefaultWorkerName(Inputs{Profiles: registry(), DefaultWorker: "gpt"}); got != "gpt" {
		t.Errorf("DefaultWorkerName = %q, want gpt", got)
	}
	if got := DefaultWorkerName(Inputs{Profiles: registry(), TaskAllowed: []string{"gone"}}); got != "" {
		t.Errorf("DefaultWorkerName on empty set = %q, want empty", got)
	}
}

func TestPolicyErrorMessage(t *testing.T) {
	e := &PolicyError{Code: CodeNotAllowed, Allowed: []string{"a", "b"}}
	if got := e.Error(); got != "profile_not_allowed: allowed: a, b" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&PolicyError{Code: CodeNoneAllowed}).Error(); got != "no_profiles_allowed" {
		t.Errorf("Error() = %q", got)
	}
}

func checkResult(t *testing.T, got string, err error, want, code string, allowed []string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("profile = %q, want %q", got, want)
		}
		return
	}
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *PolicyError %s", err, err, code)
	}
	if pe.Code != code {
		t.Errorf("code = %q, want %q (err %v)", pe.Code, code, err)
	}
	if allowed != nil && !reflect.DeepEqual(pe.Allowed, allowed) {
		t.Errorf("allowed = %v, want %v", pe.Allowed, allowed)
	}
}
