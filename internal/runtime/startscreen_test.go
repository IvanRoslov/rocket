package runtime

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readPane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// Trust and update are live captures of codex-cli 0.160.0 (--no-daemon;
// untrusted folder; update via an isolated CODEX_HOME whose version.json
// names 0.165.0); trust-compact is the orchestrator's probe capture with
// blank rows dropped. The background-server screens were not reproducible
// and are the TUI's own insta snapshots at tag rust-v0.160.0
// (daemon_recovery_menu, unmanaged_daemon_recovery_menu).
func TestParseStartScreen(t *testing.T) {
	trustOptions := []string{"Trust and continue", "Quit"}
	cases := []struct {
		fixture string
		title   string
		options []string
		context string // substring expected in Context
	}{
		{"codex-start-trust.pane", "Trust this folder?", trustOptions, "untrusted-proj"},
		{"codex-start-trust-subdir.pane", "Trust this folder?", trustOptions, "Trusting will apply to the repository root"},
		{"codex-start-trust-80col.pane", "Trust this folder?", trustOptions, "untrusted-proj"},
		{"codex-start-trust-compact.pane", "Trust this folder?", trustOptions, "codexprobe"},
		{"codex-start-update.pane", "Update available · 0.160.0 → 0.165.0",
			[]string{"Update now (runs `brew upgrade --cask codex`)", "Skip", "Skip until next version"}, "Release notes"},
		{"codex-start-daemon.pane", "Background server has incompatible feature settings",
			[]string{"Run without daemon this time", "Restart with these settings", "Cancel"}, "api_key_model_discovery = true"},
		{"codex-start-daemon-unmanaged.pane", "Background server has incompatible feature settings",
			[]string{"Run without daemon this time", "Cancel"}, "Restart may interrupt active or queued work."},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			p, ok := ParseStartScreen(readPane(t, c.fixture))
			if !ok {
				t.Fatal("ParseStartScreen = false, want a start screen")
			}
			if p.Title != c.title {
				t.Errorf("Title = %q, want %q", p.Title, c.title)
			}
			if !reflect.DeepEqual(p.Options, c.options) {
				t.Errorf("Options = %q, want %q", p.Options, c.options)
			}
			if !strings.Contains(p.Context, c.context) {
				t.Errorf("Context = %q, want it to contain %q", p.Context, c.context)
			}
			if p.Raw == "" {
				t.Error("Raw is empty")
			}
		})
	}
}

func TestParseStartScreenNegatives(t *testing.T) {
	for _, fixture := range []string{
		"codex-start-neg-idle.pane",           // ordinary Codex screen with the composer
		"codex-start-neg-update-banner.pane",  // non-blocking update banner above the normal screen
		"codex-start-neg-busy-queue.pane",     // busy Codex with its own queue block (↳)
		"permission-neg-idle.pane",            // Claude Code idle
		"permission-neg-trust.pane",           // Claude Code's own trust dialog
		"permission-bash-rm.pane",             // Claude Code permission dialog
		"permission-exit-plan.pane",           // Claude Code plan approval
		"permission-neg-numbered-output.pane", // numbered list in agent output
	} {
		t.Run(fixture, func(t *testing.T) {
			if p, ok := ParseStartScreen(readPane(t, fixture)); ok {
				t.Errorf("ParseStartScreen = %+v, true; want false", p)
			}
		})
	}
}

// A numbered list in Codex's own output followed by the composer is not a
// start screen, even if a row happens to carry the › cursor glyph.
func TestParseStartScreenComposerBelowList(t *testing.T) {
	pane := strings.Join([]string{
		"• Here is the plan:",
		"",
		"› 1. Read the code",
		"  2. Write the test",
		"",
		"› Ask Codex to do anything",
		"",
		"  GPT-6-Astra medium · ~",
	}, "\n")
	if p, ok := ParseStartScreen(pane); ok {
		t.Errorf("ParseStartScreen = %+v, true; want false", p)
	}
}

// A short heading row is not a wrapped row of the long path below it.
func TestParseStartScreenKeepsHeadingOnItsOwnLine(t *testing.T) {
	p, _ := ParseStartScreen(readPane(t, "codex-start-trust-80col.pane"))
	if !strings.HasPrefix(p.Context, "Folder access\n/private/") {
		t.Errorf("Context = %q, want the heading on its own line above the path", p.Context)
	}
}

// SameDialog tells two trust screens for different folders apart.
func TestParseStartScreenIdentityIncludesFolder(t *testing.T) {
	a, _ := ParseStartScreen(readPane(t, "codex-start-trust.pane"))
	b, _ := ParseStartScreen(readPane(t, "codex-start-trust-subdir.pane"))
	if a.SameDialog(b) {
		t.Error("trust screens for different folders compare as the same dialog")
	}
}

func TestStartScreenCursor(t *testing.T) {
	cases := map[string]int{
		"codex-start-trust.pane":            0,
		"codex-start-update.pane":           0,
		"codex-start-daemon.pane":           2,
		"codex-start-daemon-unmanaged.pane": 1,
		"codex-start-neg-idle.pane":         -1,
	}
	for fixture, want := range cases {
		if got := StartScreenCursor(readPane(t, fixture)); got != want {
			t.Errorf("%s: StartScreenCursor = %d, want %d", fixture, got, want)
		}
	}
}
