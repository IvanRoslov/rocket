package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// recordedRequest is one request a fake daemon saw.
type recordedRequest struct {
	Method, Path string
	Body         map[string]any
}

// fakeDaemon answers each "METHOD /path" from replies (status 200 with the
// JSON body, or an error envelope when the reply is an apiErrReply) and
// records every request.
type apiErrReply struct {
	status        int
	code, message string
}

func fakeDaemon(t *testing.T, replies map[string]any) (*[]recordedRequest, http.Handler) {
	t.Helper()
	var seen []recordedRequest
	return &seen, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{Method: r.Method, Path: r.URL.Path}
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.Body)
		}
		seen = append(seen, rec)
		reply, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if e, isErr := reply.(apiErrReply); isErr {
			w.WriteHeader(e.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": e.code, "message": e.message}})
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
}

var registryReply = map[string]any{"profiles": []map[string]any{
	{"name": "claude-opus", "agent": "claude-code", "model": "opus", "effort": "", "description": "hard work", "enabled": true, "position": 0},
	{"name": "codex", "agent": "codex", "model": "", "effort": "high", "description": "texts", "enabled": false, "position": 1},
	{"name": "claude-sonnet", "agent": "claude-code", "model": "sonnet", "effort": "", "description": "routine", "enabled": true, "position": 2},
}}

var settingsReply = map[string]any{
	"default_orchestrator_profile": "claude-opus",
	"default_worker_profile":       "claude-sonnet",
}

func TestModelsLsAgentSessionShowsAvailable(t *testing.T) {
	seen, h := fakeDaemon(t, map[string]any{
		"GET /v1/model-profiles/available": map[string]any{
			"profiles": []map[string]any{
				{"name": "claude-sonnet", "agent": "claude-code", "model": "sonnet", "description": "routine"},
				{"name": "codex", "agent": "codex", "effort": "high", "description": "texts"},
			},
			"default": "claude-sonnet",
		},
	})
	c := newUnixSocketTestServer(t, h)

	var out bytes.Buffer
	if err := runModelsLs(c, &out, true, false); err != nil {
		t.Fatalf("runModelsLs: %v", err)
	}
	if len(*seen) != 1 || (*seen)[0].Path != "/v1/model-profiles/available" {
		t.Fatalf("requests = %+v, want only GET /v1/model-profiles/available", *seen)
	}
	got := out.String()
	for _, want := range []string{"NAME", "DESCRIPTION", "claude-sonnet *", "routine", "codex", "high", "* = default"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestModelsLsHumanShowsRegistry(t *testing.T) {
	tests := []struct {
		name        string
		all         bool
		wantCodex   bool
		wantEnabled bool
	}{
		{"enabled only", false, false, false},
		{"--all includes disabled", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, h := fakeDaemon(t, map[string]any{
				"GET /v1/model-profiles": registryReply,
				"GET /v1/settings":       settingsReply,
			})
			c := newUnixSocketTestServer(t, h)

			var out bytes.Buffer
			if err := runModelsLs(c, &out, false, tt.all); err != nil {
				t.Fatalf("runModelsLs: %v", err)
			}
			got := out.String()
			if strings.Contains(got, "texts") != tt.wantCodex {
				t.Errorf("disabled codex shown = %v, want %v:\n%s", !tt.wantCodex, tt.wantCodex, got)
			}
			if strings.Contains(got, "ENABLED") != tt.wantEnabled {
				t.Errorf("ENABLED column shown = %v, want %v:\n%s", !tt.wantEnabled, tt.wantEnabled, got)
			}
			for _, want := range []string{"claude-opus", "orchestrator", "claude-sonnet", "worker"} {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestModelsMutationsSendRequests(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want recordedRequest
	}{
		{"add", []string{"add", "fast", "--agent", "claude-code", "--model", "haiku", "--effort", "low", "--description", "quick fixes"},
			recordedRequest{"POST", "/v1/model-profiles", map[string]any{"name": "fast", "agent": "claude-code", "model": "haiku", "effort": "low", "description": "quick fixes"}}},
		{"add minimal", []string{"add", "cx", "--agent", "codex"},
			recordedRequest{"POST", "/v1/model-profiles", map[string]any{"name": "cx", "agent": "codex"}}},
		{"edit only changed fields", []string{"edit", "fast", "--effort", "", "--disable", "--position", "4"},
			recordedRequest{"PATCH", "/v1/model-profiles/fast", map[string]any{"effort": "", "enabled": false, "position": float64(4)}}},
		{"edit enable", []string{"edit", "fast", "--enable"},
			recordedRequest{"PATCH", "/v1/model-profiles/fast", map[string]any{"enabled": true}}},
		{"rm", []string{"rm", "fast"},
			recordedRequest{"DELETE", "/v1/model-profiles/fast", nil}},
		{"default both", []string{"default", "--orchestrator", "a", "--worker", "b"},
			recordedRequest{"PUT", "/v1/settings", map[string]any{"default_orchestrator_profile": "a", "default_worker_profile": "b"}}},
		{"default worker", []string{"default", "--worker", "b"},
			recordedRequest{"PUT", "/v1/settings", map[string]any{"default_worker_profile": "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen, h := fakeDaemon(t, nil)
			c := newUnixSocketTestServer(t, h)
			cmd := newModelsCmdWith(func() (modelsClient, error) { return c, nil })
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if len(*seen) != 1 || !reflect.DeepEqual((*seen)[0], tt.want) {
				t.Errorf("requests = %+v, want [%+v]", *seen, tt.want)
			}
		})
	}
}

func TestModelsUsageErrors(t *testing.T) {
	tests := [][]string{
		{"add", "x"},                           // no --agent
		{"add"},                                // no name
		{"edit", "x"},                          // nothing to change
		{"edit", "x", "--enable", "--disable"}, // contradiction
		{"rm"},
		{"default"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newModelsCmdWith(func() (modelsClient, error) {
				t.Fatal("must not connect on a usage error")
				return nil, nil
			})
			cmd.SetArgs(args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			var ue *usageError
			if err := cmd.Execute(); !errors.As(err, &ue) {
				t.Errorf("err = %v, want usage error", err)
			}
		})
	}
}

// TestModelsHumanOnlyErrorIsExplained: an agent session hitting a
// human-only endpoint gets a sentence that says why, not a bare code.
func TestModelsHumanOnlyErrorIsExplained(t *testing.T) {
	_, h := fakeDaemon(t, map[string]any{
		"POST /v1/model-profiles": apiErrReply{403, "human_only", "only the human manages model profiles and allowlists; agent session x-orch may not"},
	})
	c := newUnixSocketTestServer(t, h)
	cmd := newModelsCmdWith(func() (modelsClient, error) { return c, nil })
	cmd.SetArgs([]string{"add", "fast", "--agent", "claude-code"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"only the human", "dashboard"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestTaskModelsShowAndSet(t *testing.T) {
	taskReply := map[string]any{"id": 12, "allowed_profiles": []string{"claude-sonnet"}, "orchestrator_profile": "claude-opus"}
	tests := []struct {
		name      string
		args      []string
		wantPatch map[string]any
		wantOut   []string
	}{
		{"show", []string{"12"}, nil, []string{"orchestrator: claude-opus", "allowed: claude-sonnet"}},
		{"allow", []string{"12", "--allow", "a, b"}, map[string]any{"allowed_profiles": []any{"a", "b"}}, nil},
		{"clear", []string{"12", "--clear"}, map[string]any{"allowed_profiles": []any{}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen, h := fakeDaemon(t, map[string]any{"GET /v1/tasks/12": taskReply, "PATCH /v1/tasks/12": taskReply})
			c := newUnixSocketTestServer(t, h)
			cmd := newTaskModelsCmdWith(func() (modelsClient, error) { return c, nil })
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			var patch *recordedRequest
			for i := range *seen {
				if (*seen)[i].Method == "PATCH" {
					patch = &(*seen)[i]
				}
			}
			if tt.wantPatch == nil {
				if patch != nil {
					t.Errorf("unexpected PATCH %+v", *patch)
				}
			} else if patch == nil || !reflect.DeepEqual(patch.Body, tt.wantPatch) {
				t.Errorf("PATCH = %+v, want body %v", patch, tt.wantPatch)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestTaskModelsUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"x"}, {"1", "--allow", "a", "--clear"}} {
		cmd := newTaskModelsCmdWith(func() (modelsClient, error) {
			t.Fatal("must not connect on a usage error")
			return nil, nil
		})
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		var ue *usageError
		if err := cmd.Execute(); !errors.As(err, &ue) {
			t.Errorf("args %v: err = %v, want usage error", args, err)
		}
	}
}

// TestLaunchCommandsHaveProfileFlags: --profile sits next to --agent on
// every launch command, and task start can narrow the allowlist.
func TestLaunchCommandsHaveProfileFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		cmd   *cobra.Command
		flags []string
	}{
		"spawn":      {newSpawnCmd(), []string{"profile", "agent"}},
		"up":         {newUpCmd(), []string{"profile", "agent"}},
		"task start": {newTaskStartCmd(), []string{"profile", "agent", "allow"}},
	} {
		for _, f := range tc.flags {
			if tc.cmd.Flags().Lookup(f) == nil {
				t.Errorf("%s has no --%s flag", name, f)
			}
		}
	}
}

func TestStartRequestBody(t *testing.T) {
	tests := []struct {
		agent, profile, allow string
		want                  map[string]any
	}{
		{"", "", "", nil},
		{"codex", "", "", map[string]any{"agent": "codex"}},
		{"", "claude-opus", "a,b", map[string]any{"profile": "claude-opus", "allowed_profiles": []string{"a", "b"}}},
	}
	for _, tt := range tests {
		if got := startRequestBody(tt.agent, tt.profile, tt.allow); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("startRequestBody(%q,%q,%q) = %v, want %v", tt.agent, tt.profile, tt.allow, got, tt.want)
		}
	}
}

func TestRenderStatusShowsProfiles(t *testing.T) {
	now := time.Now()
	var out bytes.Buffer
	renderStatus("feat", []sessionRow{
		{ID: "feat-orch", Kind: "orchestrator", State: "running", Profile: "claude-opus", CreatedAt: now.Unix()},
		{ID: "feat-w1", Kind: "worker", State: "running", Profile: "claude-sonnet", CreatedAt: now.Unix()},
		{ID: "feat-w2", Kind: "worker", State: "running", CreatedAt: now.Unix()},
	}, nil, &out, now)
	got := out.String()
	for _, want := range []string{"orchestrator: feat-orch [running] claude-opus", "PROFILE", "claude-sonnet"} {
		if !strings.Contains(got, want) {
			t.Errorf("status missing %q:\n%s", want, got)
		}
	}
}
