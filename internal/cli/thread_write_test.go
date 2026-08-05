package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCloseRequestBody: the three ways to close a thread, each rendered into
// the request the API already understands (task #1023, spec v1 §«Глаголы»).
func TestCloseRequestBody(t *testing.T) {
	tests := []struct {
		name string
		opts threadCloseOptions
		want map[string]any
	}{
		{
			name: "с ответом",
			opts: threadCloseOptions{body: "берём вариант A"},
			want: map[string]any{"body": "берём вариант A"},
		},
		{
			name: "выбором варианта",
			opts: threadCloseOptions{choose: 2},
			want: map[string]any{"choose": 2},
		},
		{
			name: "как неактуальный",
			opts: threadCloseOptions{dismiss: true},
			want: map[string]any{"dismiss": true},
		},
		{
			name: "как неактуальный с причиной",
			opts: threadCloseOptions{dismiss: true, body: "уже решили в чате"},
			want: map[string]any{"dismiss": true, "body": "уже решили в чате"},
		},
		{
			name: "с адресатами и подтверждениями",
			opts: threadCloseOptions{body: "ок", to: []string{"cto"}, join: true, dryRun: true},
			want: map[string]any{"body": "ок", "to": []string{"cto"}, "join": true, "dry_run": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.requestBody(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("requestBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestReplyRequestBody: a reply carries no resolution, only the text and the
// two confirmation flags.
func TestReplyRequestBody(t *testing.T) {
	got := threadReplyOptions{body: "смотрю", to: []string{"human"}, join: true}.requestBody()
	want := map[string]any{"body": "смотрю", "to": []string{"human"}, "join": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requestBody() = %v, want %v", got, want)
	}
	// No --to means no "to" key at all, keeping the request identical to what
	// the CLI sent before addressees existed.
	if got := (threadReplyOptions{body: "x"}).requestBody(); len(got) != 1 {
		t.Errorf("requestBody() = %v, want only a body", got)
	}
}

// TestCloseOptionsValidate: the mutually exclusive ways to say what the
// resolution is. Picking two at once is a typo, and guessing which one the
// user meant is how a thread gets closed with the wrong answer.
func TestCloseOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    threadCloseOptions
		wantErr bool
	}{
		{"ответ", threadCloseOptions{body: "текст"}, false},
		{"выбор", threadCloseOptions{choose: 1}, false},
		{"dismiss", threadCloseOptions{dismiss: true}, false},
		{"dismiss с причиной", threadCloseOptions{dismiss: true, body: "почему"}, false},
		{"выбор и ответ вместе", threadCloseOptions{choose: 1, body: "текст"}, true},
		{"выбор и dismiss вместе", threadCloseOptions{choose: 1, dismiss: true}, true},
		{"ничего", threadCloseOptions{}, true},
		{"отрицательный выбор", threadCloseOptions{choose: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate("usage line")
			if tt.wantErr != (err != nil) {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				var usageErr *usageError
				if !errors.As(err, &usageErr) {
					t.Errorf("expected usageError, got %T", err)
				}
			}
		})
	}
}

// TestRenderWriteResult: every write says WHERE it landed, so a misaddressed
// reply is visible immediately rather than hours later (spec v1
// §«Подтверждение цели»).
func TestRenderWriteResult(t *testing.T) {
	echo := `→ 1023/Q2 «Какой подход?» (task #1023 "Ship it")`

	got := renderWriteResult("reply added to", questionRow{LocalRef: "1023/Q2", Echo: echo})
	if !strings.Contains(got, "1023/Q2") || !strings.Contains(got, echo) {
		t.Errorf("result = %q, want the ref and the echo", got)
	}

	// A dry run wrote nothing and must not claim otherwise.
	dry := renderWriteResult("reply added to", questionRow{LocalRef: "1023/Q2", Echo: echo, DryRun: true})
	if strings.Contains(dry, "reply added to") {
		t.Errorf("dry run result = %q, must not claim a write happened", dry)
	}
	if !strings.Contains(dry, echo) || !strings.Contains(dry, "ничего не отправлено") {
		t.Errorf("dry run result = %q, want the echo and an explicit no-op note", dry)
	}

	// A daemon that predates the echo still gets a usable line.
	if got := renderWriteResult("reply added to", questionRow{LocalRef: "1023/Q2"}); !strings.Contains(got, "1023/Q2") {
		t.Errorf("result without echo = %q, want the ref", got)
	}
}

// TestCloseCmdReplacesAnswer: close is the verb; answer survives as a hidden
// alias so existing scripts and prompts keep working.
func TestCloseCmdReplacesAnswer(t *testing.T) {
	for _, group := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{"task", newTaskCmd()},
		{"agent", newAgentCmd()},
	} {
		t.Run(group.name, func(t *testing.T) {
			var closeCmd, answerCmd *cobra.Command
			for _, c := range group.cmd.Commands() {
				switch c.Name() {
				case "close":
					closeCmd = c
				case "answer":
					answerCmd = c
				}
			}
			if closeCmd == nil {
				t.Fatalf("expected a `%s close` command", group.name)
			}
			if closeCmd.Hidden {
				t.Errorf("close must be visible in help")
			}
			for _, flag := range []string{"dismiss", "choose", "dry-run", "join", "file", "to"} {
				if closeCmd.Flags().Lookup(flag) == nil {
					t.Errorf("expected --%s on %s close", flag, group.name)
				}
			}
			if answerCmd == nil {
				t.Fatalf("expected `%s answer` to survive as an alias", group.name)
			}
			if !answerCmd.Hidden {
				t.Errorf("answer must be hidden from help now that close is the verb")
			}
		})
	}
}

// TestReplyCmdHasConfirmationFlags: --dry-run and --join live on the writes
// that can land in the wrong thread. Opening your own thread cannot, so ask
// deliberately has neither.
func TestReplyCmdHasConfirmationFlags(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"task reply":  newTaskReplyCmd(),
		"agent reply": newAgentReplyCmd(),
	} {
		for _, flag := range []string{"dry-run", "join"} {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("expected --%s on %s", flag, name)
			}
		}
	}
	for name, cmd := range map[string]*cobra.Command{
		"task ask":      newTaskAskCmd(),
		"task ask-orch": newTaskAskOrchCmd(),
		"agent ask":     newAgentAskCmd(),
	} {
		for _, flag := range []string{"dry-run", "join"} {
			if cmd.Flags().Lookup(flag) != nil {
				t.Errorf("--%s must not be on %s: opening your own thread cannot be misaddressed", flag, name)
			}
		}
	}
}

// TestAskRequestBodyOptionsAndFyi: --option and --fyi ride the fields the API
// already accepts, and their absence leaves the request exactly as before.
func TestAskRequestBodyOptionsAndFyi(t *testing.T) {
	got := askRequestBody("вопрос?", "контекст", []string{"cto"}, []string{"A", "B"}, false)
	want := map[string]any{
		"body": "вопрос?", "context": "контекст", "to": []string{"cto"},
		"options": []string{"A", "B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("askRequestBody = %v, want %v", got, want)
	}

	fyi := askRequestBody("выкатили", "", nil, nil, true)
	if !reflect.DeepEqual(fyi, map[string]any{"body": "выкатили", "type": "fyi"}) {
		t.Errorf("askRequestBody fyi = %v", fyi)
	}

	plain := askRequestBody("вопрос?", "", nil, nil, false)
	if !reflect.DeepEqual(plain, map[string]any{"body": "вопрос?"}) {
		t.Errorf("askRequestBody plain = %v, want only a body", plain)
	}
}

// TestAskOptionsRejectedOnFyi: an fyi thread is a status note nobody answers,
// so offering it answer choices is a contradiction rather than a no-op.
func TestAskOptionsRejectedOnFyi(t *testing.T) {
	err := validateAskFlags([]string{"A"}, true, "usage line")
	if err == nil {
		t.Fatal("expected --fyi with --option to be rejected")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Errorf("expected usageError, got %T", err)
	}
	if err := validateAskFlags([]string{"A"}, false, "usage line"); err != nil {
		t.Errorf("options without --fyi must be fine, got %v", err)
	}
}

// TestAskCmdHasOptionAndFyi: all three ask commands offer them.
func TestAskCmdHasOptionAndFyi(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"task ask":      newTaskAskCmd(),
		"task ask-orch": newTaskAskOrchCmd(),
		"agent ask":     newAgentAskCmd(),
	} {
		for _, flag := range []string{"option", "fyi"} {
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("expected --%s on %s", flag, name)
			}
		}
	}
}

// newTaskCloseCmd0 / newAgentCloseCmd0 build the VISIBLE close verb, for tests
// that exercise the command rather than its hidden alias.
func newTaskCloseCmd0() *cobra.Command  { return newTaskCloseCmd(false) }
func newAgentCloseCmd0() *cobra.Command { return newAgentCloseCmd(false) }
