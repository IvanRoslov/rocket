package runtime

import (
	"reflect"
	"strings"
	"testing"
)

// The scratch path every recon capture's "allow access to …" option spells
// out, split across rows by the TUI and rejoined by the parser.
const reconScratch = "/private/tmp/claude-501/-Users-ivanroslov--rocket-worktrees-rocket-task-4881-prompt-parser/e5294f83-7c4f-447b-810f-3d4cb32de35a/scratchpad/recon"

// TestParsePermissionPromptOnLiveCaptures runs the parser over panes
// recorded from Claude Code 2.1.286 (docs/superpowers/recon/
// 2026-10-01-permission-prompt-recon.md).
func TestParsePermissionPromptOnLiveCaptures(t *testing.T) {
	acceptEdits := "Yes, and switch to accept edits (auto-approve file edits and common file commands) for this session (shift+tab)"
	autoMode := "Yes, and switch to auto mode · auto mode handles these prompts for you"
	planOptions := []string{
		"Yes, and switch to BYPASS PERMISSIONS (no further prompts) for this session",
		"Yes, manually approve edits",
	}
	planContext := "Ready to code?\n\nHere is Claude's plan:\nCreate file plan.txt containing ok"

	cases := []struct {
		fixture string
		title   string
		context string
		options []string
	}{
		{
			fixture: "permission-edit-settings.pane",
			title:   "Do you want to make this edit to settings.json?",
			context: "Edit file\n.claude/settings.json\n1 -{}\n1 +{\"env\": {\"FOO\": \"1\"}}",
			options: []string{"Yes", "Yes, and allow Claude to edit files in this project's .claude folder for this session", "No"},
		},
		{
			fixture: "permission-edit-settings-ansi.pane",
			title:   "Do you want to make this edit to settings.json?",
			context: "Edit file\n.claude/settings.json\n1 -{}\n1 +{\"env\": {\"FOO\": \"1\"}}",
			options: []string{"Yes", "Yes, and allow Claude to edit files in this project's .claude folder for this session", "No"},
		},
		{
			fixture: "permission-edit-file.pane",
			title:   "Do you want to make this edit to notes.txt?",
			context: "Edit file\nnotes.txt\n1 -hello\n1 +hello world",
			options: []string{"Yes", acceptEdits, "No"},
		},
		{
			fixture: "permission-create-file.pane",
			title:   "Do you want to create greet.txt?",
			context: "Create file\nsrc/greet.txt\n 1 hi",
			options: []string{"Yes", acceptEdits, "No"},
		},
		{
			fixture: "permission-bash-outside-cwd.pane",
			title:   "Do you want to proceed?",
			context: "Bash command\nList parent directory contents\nls -la ../ && echo done",
			options: []string{"Yes", "Yes, allow reading from " + reconScratch + " from this project", autoMode, "No"},
		},
		{
			fixture: "permission-bash-rm.pane",
			title:   "Do you want to proceed?",
			context: "Bash command\nDelete the build directory\nrm -rf ./build",
			options: []string{"Yes", "Yes, and always allow access to " + reconScratch + "/proj/build from this project", autoMode, "No"},
		},
		{
			fixture: "permission-bash-multiline.pane",
			title:   "Do you want to proceed?",
			context: "Bash command\nCreate out directory with thirteen empty files\n│ mkdir -p out\n│ touch out/f1\n│ touch out/f2\n│ touch out/f3\n│ touch out/f4\n│ touch out/f5\n│ touch out/f6\n│ touch out/f7\n…",
			options: []string{
				"Yes",
				"Yes, and don't ask again for mkdir -p out, touch out/f1, touch out/f2, touch out/f3, and touch out/f4 commands in " + reconScratch + "/proj",
				autoMode,
				"No",
			},
		},
		{
			fixture: "permission-narrow-60col.pane",
			title:   "Do you want to proceed?",
			context: "Bash command\nCreate an empty test file\ntouch ./narrow-test-file-with-a-rather-long-name.txt",
			options: []string{"Yes", "Yes, and always allow access to " + reconScratch + "/proj from this project", autoMode, "No"},
		},
		{
			fixture: "permission-webfetch.pane",
			title:   "Do you want to allow Claude to fetch this content?",
			context: "Fetch\nClaude wants to fetch content from example.com\nurl: https://example.com/\nprompt: What is the page's title?",
			options: []string{"Yes", "Yes, and don't ask again for example.com", "No, and tell Claude what to do differently (esc)"},
		},
		{
			fixture: "permission-exit-plan.pane",
			title:   "Claude has written up a plan and is ready to execute. Would you like to proceed?",
			context: planContext,
			options: planOptions,
		},
		{
			// The cursor sits on the free-text option: still the same dialog,
			// and that option still is not a one-keypress answer.
			fixture: "permission-exit-plan-cursor3.pane",
			title:   "Claude has written up a plan and is ready to execute. Would you like to proceed?",
			context: planContext,
			options: planOptions,
		},
		{
			fixture: "permission-after-plan-create.pane",
			title:   "Do you want to create plan.txt?",
			context: "Create file\nplan.txt\n 1 ok",
			options: []string{"Yes", acceptEdits, "No"},
		},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			got, ok := ParsePermissionPrompt(fixture(t, c.fixture))
			if !ok {
				t.Fatalf("ParsePermissionPrompt ok = false, want true")
			}
			if got.Title != c.title {
				t.Errorf("Title = %q, want %q", got.Title, c.title)
			}
			if got.Context != c.context {
				t.Errorf("Context = %q, want %q", got.Context, c.context)
			}
			if !reflect.DeepEqual(got.Options, c.options) {
				t.Errorf("Options = %q, want %q", got.Options, c.options)
			}
			if got.Raw == "" || strings.Contains(got.Raw, "\x1b") {
				t.Errorf("Raw = %q, want the visible pane tail without escapes", got.Raw)
			}
			if !strings.Contains(got.Raw, c.title) {
				t.Errorf("Raw does not contain the title; Raw = %q", got.Raw)
			}
		})
	}
}

// Things at the bottom of a pane that look like a numbered selector or a
// question but are not a permission dialog.
func TestParsePermissionPromptRejects(t *testing.T) {
	cases := []struct {
		name string
		pane string
	}{
		{"held peer dialog", fixture(t, "held-dialog-open.pane")},
		{"AskUserQuestion widget", fixture(t, "permission-neg-quiz.pane")},
		// Same shape as a permission dialog; only the widget footer tells.
		{"AskUserQuestion widget without the chat rule",
			" ☐ Colour\nWhich colour?\n❯ 1. Red\n  2. Green\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"},
		{"numbered list in agent output above the composer", fixture(t, "permission-neg-numbered-output.pane")},
		{"idle composer", fixture(t, "permission-neg-idle.pane")},
		{"folder trust dialog", fixture(t, "permission-neg-trust.pane")},
		{"idle composer, boxed layout", idleComposerPane},
		{"prompt text echoed above the composer",
			"⏺ Do you want to proceed?\n  ❯ 1. Yes\n    2. No\n\n" +
				"────────────────────────────────────────\n❯ \n────────────────────────────────────────\n" +
				"  ⏵⏵ bypass permissions on (shift+tab to cycle)"},
		{"selector without a question", " Pick one\n ❯ 1. Yes\n   2. No\n"},
		{"selector with a single option", " Continue?\n ❯ 1. Yes\n"},
		{"numbering does not start at 1", " Continue?\n ❯ 2. Yes\n   3. No\n"},
		{"no cursor in the list", " Continue?\n   1. Yes\n   2. No\n"},
		{"empty", ""},
		{"blank rows only", "\n\n   \n"},
	}
	for _, c := range cases {
		if got, ok := ParsePermissionPrompt(c.pane); ok {
			t.Errorf("%s: ParsePermissionPrompt = %+v, true; want false", c.name, got)
		}
	}
}

// The pre-2.1 boxed layout (the one inputwait_test.go models) parses too.
func TestParsePermissionPromptBoxedLayout(t *testing.T) {
	got, ok := ParsePermissionPrompt(permissionPromptPane)
	if !ok {
		t.Fatalf("ParsePermissionPrompt ok = false, want true")
	}
	if got.Title != "Do you want to proceed?" {
		t.Errorf("Title = %q", got.Title)
	}
	want := []string{"Yes", "Yes, and don't ask again for rm commands in this dir", "No, and tell Claude what to do differently (esc)"}
	if !reflect.DeepEqual(got.Options, want) {
		t.Errorf("Options = %q, want %q", got.Options, want)
	}
	if !strings.Contains(got.Context, "rm -rf build") {
		t.Errorf("Context = %q, want it to carry the command", got.Context)
	}
}

// A dialog whose question is recognisable but whose choices cannot be mapped
// to digit keys still surfaces — with no options, so the client falls back
// to Raw plus Esc.
func TestParsePermissionPromptFallback(t *testing.T) {
	cases := []struct {
		name  string
		pane  string
		title string
	}{
		{"unnumbered choices",
			"────────────────────\n Bash command\n rm -rf build\n\n Do you want to proceed?\n ❯ Yes\n   No\n\n Esc to cancel\n",
			"Do you want to proceed?"},
		{"free-text option before the last one",
			"────────────────────\n Would you like to proceed?\n ❯ 1. Tell Claude what to change\n      shift+tab to approve with this feedback\n   2. Yes\n",
			"Would you like to proceed?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParsePermissionPrompt(c.pane)
			if !ok {
				t.Fatalf("ParsePermissionPrompt ok = false, want true")
			}
			if got.Title != c.title {
				t.Errorf("Title = %q, want %q", got.Title, c.title)
			}
			if len(got.Options) != 0 {
				t.Errorf("Options = %q, want none", got.Options)
			}
			if !strings.Contains(got.Raw, c.title) {
				t.Errorf("Raw = %q, want the pane tail", got.Raw)
			}
		})
	}
}

// The free-text hint is recognised by its wording, not by geometry: in a
// narrow pane the hint row would otherwise pass for a wrapped label.
func TestParsePermissionPromptFreeTextHintInNarrowPane(t *testing.T) {
	pane := "──────────────────────────────────────\n" +
		" Would you like to proceed?\n" +
		" ❯ 1. Yes\n" +
		"   2. Tell Claude what to change and more\n" +
		"      shift+tab to approve with this\n" +
		"      feedback\n"
	got, ok := ParsePermissionPrompt(pane)
	if !ok {
		t.Fatalf("ParsePermissionPrompt ok = false, want true")
	}
	if want := []string{"Yes"}; !reflect.DeepEqual(got.Options, want) {
		t.Errorf("Options = %q, want %q", got.Options, want)
	}
}

// Raw is bounded: the client shows it verbatim, so it must not grow with the
// scrollback.
func TestParsePermissionPromptRawIsPaneTail(t *testing.T) {
	got, ok := ParsePermissionPrompt(fixture(t, "permission-bash-multiline.pane"))
	if !ok {
		t.Fatalf("ParsePermissionPrompt ok = false, want true")
	}
	rows := strings.Split(got.Raw, "\n")
	if len(rows) != permissionRawRows {
		t.Errorf("Raw has %d rows, want %d", len(rows), permissionRawRows)
	}
	if last := rows[len(rows)-1]; strings.TrimSpace(last) != "Esc to cancel · Tab to amend" {
		t.Errorf("Raw ends with %q, want the dialog footer", last)
	}
}

// TestPermissionPromptSameDialog pins the dialog identity the monitor and
// the answer path compare: Title plus Context. Two Bash dialogs share the
// title «Do you want to proceed?» and differ only in Context.
func TestPermissionPromptSameDialog(t *testing.T) {
	rm, ok := ParsePermissionPrompt(fixture(t, "permission-bash-rm.pane"))
	if !ok {
		t.Fatal("bash-rm fixture not parsed")
	}
	outside, ok := ParsePermissionPrompt(fixture(t, "permission-bash-outside-cwd.pane"))
	if !ok {
		t.Fatal("bash-outside-cwd fixture not parsed")
	}
	if rm.Title != outside.Title {
		t.Fatalf("fixtures no longer share a title: %q vs %q", rm.Title, outside.Title)
	}
	if rm.SameDialog(outside) {
		t.Errorf("SameDialog = true for two Bash dialogs with different commands")
	}

	moved := rm
	moved.Options = append([]string(nil), rm.Options...)
	moved.Options[0] = "something else"
	moved.Raw = "different raw"
	if !rm.SameDialog(moved) {
		t.Errorf("SameDialog = false for the same Title+Context with different Options/Raw")
	}
}

// TestPermissionCaptureLinesHoldsWholeDialog checks the shared capture depth
// is deep enough that a dialog read at that depth has the same identity as
// the full pane — the condition that keeps the monitor's read and the
// pre-press re-read from disagreeing.
func TestPermissionCaptureLinesHoldsWholeDialog(t *testing.T) {
	for _, fx := range []string{"permission-bash-multiline.pane", "permission-bash-rm.pane", "permission-edit-settings.pane", "permission-exit-plan.pane"} {
		pane := fixture(t, fx)
		full, ok := ParsePermissionPrompt(pane)
		if !ok {
			t.Fatalf("%s: not parsed", fx)
		}
		rows := strings.Split(strings.TrimRight(pane, "\n"), "\n")
		if len(rows) > PermissionCaptureLines {
			rows = rows[len(rows)-PermissionCaptureLines:]
		}
		cut, ok := ParsePermissionPrompt(strings.Join(rows, "\n"))
		if !ok || !full.SameDialog(cut) {
			t.Errorf("%s: dialog read at %d rows differs from the full pane (ok=%v)\nfull=%q\ncut=%q", fx, PermissionCaptureLines, ok, full.Context, cut.Context)
		}
	}
}
