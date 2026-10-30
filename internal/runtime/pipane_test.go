package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePiComposerSnapshots(t *testing.T) {
	for name, want := range map[string]PiComposer{
		"pi-idle.pane":        PiEmpty,
		"pi-cleared.pane":     PiEmpty,
		"pi-finished.pane":    PiEmpty,
		"pi-plain-draft.pane": PiDraft,
		"pi-draft.pane":       PiDraft,
		"pi-busy.pane":        PiBusy,
		"pi-busy-queued.pane": PiBusy,
	} {
		got, ok := ParsePiComposer(readPane(t, name))
		if !ok || got != want {
			t.Errorf("%s: ParsePiComposer = %v, %v; want %v, true", name, got, ok, want)
		}
	}
}

func TestParsePiComposerNegatives(t *testing.T) {
	all, err := filepath.Glob("testdata/*.pane")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range all {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "pi-") {
			continue
		}
		if got, ok := ParsePiComposer(readPane(t, name)); ok {
			t.Errorf("%s: ParsePiComposer = %v, true; want not recognized", name, got)
		}
	}
	for _, pane := range []string{"", "a completely unknown pane\nwith no composer"} {
		if got, ok := ParsePiComposer(pane); ok {
			t.Errorf("unknown pane = %v, true; want not recognized", got)
		}
	}
}

func TestLooksLikePiQueued(t *testing.T) {
	busy := readPane(t, "pi-busy-queued.pane")
	if !LooksLikePiQueued(busy, "<redacted-steering>") {
		t.Error("current steering not recognized")
	}
	if LooksLikePiQueued(busy, "something else entirely") {
		t.Error("different text recognized as steering")
	}
	if LooksLikePiQueued(readPane(t, "pi-finished.pane"), "<redacted-steering>") {
		t.Error("finished scrollback recognized as a live queue")
	}
	if LooksLikePiQueued(readPane(t, "pi-draft.pane"), "<redacted-steering>") {
		t.Error("draft pane recognized as a live queue")
	}
}
