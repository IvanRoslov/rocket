package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKimiComposerSnapshots(t *testing.T) {
	cases := map[string]KimiComposer{
		"kimi-idle.pane":        KimiEmpty,
		"kimi-draft.pane":       KimiDraft,
		"kimi-busy-queued.pane": KimiBusy,
		"kimi-finished.pane":    KimiEmpty,
		"kimi-agentsmd.pane":    KimiEmpty,
	}
	for name, want := range cases {
		got, ok := ParseKimiComposer(readPane(t, name))
		if !ok || got != want {
			t.Errorf("%s: ParseKimiComposer = %v, %v; want %v", name, got, ok, want)
		}
	}
}

func TestParseKimiComposerNegatives(t *testing.T) {
	names := []string{"kimi-trust.pane", "kimi-question.pane"}
	all, err := filepath.Glob("testdata/*.pane")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if !strings.HasPrefix(filepath.Base(p), "kimi-") {
			names = append(names, filepath.Base(p))
		}
	}
	for _, name := range names {
		if got, ok := ParseKimiComposer(readPane(t, name)); ok {
			t.Errorf("%s: ParseKimiComposer = %v, true; want not recognized", name, got)
		}
	}
	if _, ok := ParseKimiComposer(""); ok {
		t.Error("empty pane recognized")
	}
}

// A draft typed over several rows, and the pane captured with escapes.
func TestParseKimiComposerMultilineDraftWithEscapes(t *testing.T) {
	pane := strings.Join([]string{
		" \x1b[2m╭──────────────╮\x1b[0m",
		" │ > \x1b[1mfirst\x1b[0m       │",
		" │   second     │",
		" ╰──────────────╯",
		" Never Ask  K3.1 Pro",
		"            context: 0% (0/1M)",
	}, "\n")
	if got, ok := ParseKimiComposer(pane); !ok || got != KimiDraft {
		t.Fatalf("got %v, %v", got, ok)
	}
	empty := strings.Replace(strings.Replace(pane, "\x1b[1mfirst\x1b[0m", "     ", 1), "second", "      ", 1)
	if got, ok := ParseKimiComposer(empty); !ok || got != KimiEmpty {
		t.Fatalf("empty: got %v, %v", got, ok)
	}
}

func TestParseKimiStartScreenTrust(t *testing.T) {
	p, ok := ParseKimiStartScreen(readPane(t, "kimi-trust.pane"))
	if !ok {
		t.Fatal("trust screen not recognized")
	}
	if p.Title != "Trust this folder?" {
		t.Errorf("Title = %q", p.Title)
	}
	if len(p.Options) != 2 || p.Options[0] != "Trust and continue" || p.Options[1] != "Exit" {
		t.Errorf("Options = %q", p.Options)
	}
	if !strings.Contains(p.Context, "/.rocket/scratch/kimi-recon/untrusted") {
		t.Errorf("Context lacks the rejoined path: %q", p.Context)
	}
	if strings.Contains(p.Context, "navigate") {
		t.Errorf("Context holds the key hint: %q", p.Context)
	}
	if p.Raw == "" {
		t.Error("Raw empty")
	}
	if c := KimiStartScreenCursor(readPane(t, "kimi-trust.pane")); c != 0 {
		t.Errorf("cursor = %d, want 0", c)
	}
	moved := strings.Replace(strings.Replace(readPane(t, "kimi-trust.pane"), "   ❯ Trust and continue", "     Trust and continue", 1), "     Exit", "   ❯ Exit", 1)
	if c := KimiStartScreenCursor(moved); c != 1 {
		t.Errorf("moved cursor = %d, want 1", c)
	}
	if q, ok := ParseKimiStartScreen(moved); !ok || !q.SameDialog(p) {
		t.Error("moving the cursor changed the dialog identity")
	}
}

func TestKimiCacheHintCompactSelected(t *testing.T) {
	pane := readPane(t, "kimi-cache-hint.pane")
	if !KimiCacheHintDialog(pane) || !KimiCacheHintCompactSelected(pane) {
		t.Fatal("cache hint with compact selected not recognized")
	}
	moved := strings.Replace(strings.Replace(pane, "   ❯ Compact and continue", "     Compact and continue", 1), "     Continue as-is", "   ❯ Continue as-is", 1)
	if !KimiCacheHintDialog(moved) {
		t.Fatal("cache hint not recognized after the cursor left compact")
	}
	if KimiCacheHintCompactSelected(moved) {
		t.Fatal("compact recognized as selected after the cursor moved")
	}
	for _, name := range []string{"kimi-trust.pane", "kimi-question.pane", "kimi-idle.pane"} {
		other := readPane(t, name)
		if KimiCacheHintDialog(other) || KimiCacheHintCompactSelected(other) {
			t.Errorf("%s recognized as a cache hint", name)
		}
	}
}

// Kimi's AskUserQuestion widget is a journal interaction (Activity reports
// blocked), not a start screen; composer and other agents' panes are not
// start screens either.
func TestParseKimiStartScreenNegatives(t *testing.T) {
	all, err := filepath.Glob("testdata/*.pane")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		name := filepath.Base(p)
		if name == "kimi-trust.pane" || name == "kimi-cache-hint.pane" {
			continue
		}
		if got, ok := ParseKimiStartScreen(readPane(t, name)); ok {
			t.Errorf("%s: recognized as Kimi start screen %+v", name, got)
		}
		if KimiStartScreenCursor(readPane(t, name)) != -1 {
			t.Errorf("%s: cursor without a screen", name)
		}
	}
}

// Codex's start-screen parser must keep ignoring Kimi's screen (its options
// are unnumbered) and vice versa.
func TestCodexStartScreenIgnoresKimiTrust(t *testing.T) {
	if _, ok := ParseStartScreen(readPane(t, "kimi-trust.pane")); ok {
		t.Fatal("Codex parser matched Kimi trust screen")
	}
}
