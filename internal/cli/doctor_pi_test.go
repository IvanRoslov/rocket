package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stubPiList(t *testing.T, output string, err error) {
	t.Helper()
	old := runPiList
	t.Cleanup(func() { runPiList = old })
	runPiList = func(context.Context) ([]byte, error) { return []byte(output), err }
}

func writeDoctorBinary(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPiSuperpowers(t *testing.T) {
	cases := []struct {
		name   string
		output string
		err    error
		status checkStatus
		detail string
	}{
		{"installed", "superpowers git:github.com/obra/superpowers\n", nil, statusOK, "установлен"},
		{"missing", "other-package git:example.com/other\n", nil, statusWarn, "pi install git:github.com/obra/superpowers"},
		{"list error", "", errors.New("pi list failed"), statusWarn, "pi install git:github.com/obra/superpowers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubPiList(t, tc.output, tc.err)
			r := checkPiSuperpowers()
			if r.Name != "pi:superpowers" || r.Status != tc.status || !strings.Contains(r.Detail, tc.detail) {
				t.Fatalf("got %+v, want name pi:superpowers status %v detail ~ %q", r, tc.status, tc.detail)
			}
		})
	}
}

func TestCheckPiSuperpowersTimeout(t *testing.T) {
	oldTimeout := piListTimeout
	piListTimeout = time.Millisecond
	t.Cleanup(func() { piListTimeout = oldTimeout })
	stubPiList(t, "", context.DeadlineExceeded)
	r := checkPiSuperpowers()
	if r.Status != statusWarn || !strings.Contains(r.Detail, "pi install git:github.com/obra/superpowers") {
		t.Fatalf("timeout result = %+v", r)
	}
}

func TestCheckPiSuperpowersNeverPrintsCredentials(t *testing.T) {
	const secret = "doctor-secret-value"
	t.Setenv("DEEPSEEK_API_KEY", secret)
	stubPiList(t, secret, errors.New(secret))
	r := checkPiSuperpowers()
	if strings.Contains(r.Detail, secret) {
		t.Fatalf("detail leaked a credential: %q", r.Detail)
	}
}

func TestCheckPiAgentAvailability(t *testing.T) {
	t.Run("unusable Pi skips the extra check", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if got := checkPiAgent(); len(got) != 0 {
			t.Fatalf("results = %+v, want none", got)
		}
	})

	t.Run("usable Pi checks Superpowers", func(t *testing.T) {
		dir := t.TempDir()
		writeDoctorBinary(t, dir, "pi", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 1.0.0; fi\n")
		t.Setenv("PATH", dir)
		stubPiList(t, "git:github.com/obra/superpowers", nil)
		got := checkPiAgent()
		if len(got) != 1 || got[0].Name != "pi:superpowers" || got[0].Status != statusOK {
			t.Fatalf("results = %+v", got)
		}
	})
}

func TestDoctorDeepSeekGenericAvailability(t *testing.T) {
	t.Run("missing binary", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv("DEEPSEEK_API_KEY", "present")
		for _, r := range checkAgents() {
			if r.Name == "agent:deepseek" {
				if r.Status != statusFail || r.Detail == "" {
					t.Fatalf("result = %+v", r)
				}
				return
			}
		}
		t.Fatal("agent:deepseek result missing")
	})

	t.Run("missing key", func(t *testing.T) {
		dir := t.TempDir()
		writeDoctorBinary(t, dir, "claude", "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", dir)
		t.Setenv("DEEPSEEK_API_KEY", "")
		for _, r := range checkAgents() {
			if r.Name == "agent:deepseek" {
				if r.Status != statusFail || !strings.Contains(r.Detail, "DEEPSEEK_API_KEY is required") {
					t.Fatalf("result = %+v", r)
				}
				return
			}
		}
		t.Fatal("agent:deepseek result missing")
	})
}
