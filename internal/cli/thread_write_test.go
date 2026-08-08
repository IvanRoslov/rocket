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
	got := askRequestBody("", "вопрос?", "контекст", []string{"cto"}, []string{"A", "B"}, false)
	want := map[string]any{
		"body": "вопрос?", "context": "контекст", "to": []string{"cto"},
		"options": []string{"A", "B"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("askRequestBody = %v, want %v", got, want)
	}

	fyi := askRequestBody("", "выкатили", "", nil, nil, true)
	if !reflect.DeepEqual(fyi, map[string]any{"body": "выкатили", "type": "fyi"}) {
		t.Errorf("askRequestBody fyi = %v", fyi)
	}

	plain := askRequestBody("", "вопрос?", "", nil, nil, false)
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

// TestReplyRequestBody_Dispute: --dispute is the only thing that reopens a
// resolved thread, and its absence must leave the request byte-identical to
// the one the CLI sent before the flag existed (subtask #1181).
func TestReplyRequestBody_Dispute(t *testing.T) {
	got := threadReplyOptions{body: "ответ неверен", dispute: true}.requestBody()
	want := map[string]any{"body": "ответ неверен", "dispute": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requestBody() = %v, want %v", got, want)
	}
	if got := (threadReplyOptions{body: "принял"}).requestBody(); len(got) != 1 {
		t.Errorf("requestBody() = %v, want only a body", got)
	}
}

// TestDisputeHint: a reply that landed in a thread which stayed resolved says
// so, because otherwise the author has no way to tell an accepted ack from a
// dispute that silently did nothing.
func TestDisputeHint(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		dispute bool
		dryRun  bool
		want    bool
	}{
		{"ack в закрытый тред", "resolved", false, false, true},
		{"оспаривание", "resolved", true, false, false},
		{"открытый тред", "open", false, false, false},
		{"репетиция", "resolved", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := disputeHint(tt.status, tt.dispute, tt.dryRun)
			if (got != "") != tt.want {
				t.Errorf("disputeHint(%q, %v, %v) = %q", tt.status, tt.dispute, tt.dryRun, got)
			}
			if tt.want && !strings.Contains(got, "--dispute") {
				t.Errorf("подсказка должна называть флаг: %q", got)
			}
		})
	}
}

// TestReplyCommandsHaveDispute: both reply surfaces expose the flag, or the
// two drift apart and the same act needs a different incantation per thread
// kind.
func TestReplyCommandsHaveDispute(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"task reply":  newTaskReplyCmd(),
		"agent reply": newAgentReplyCmd(),
	} {
		if cmd.Flags().Lookup("dispute") == nil {
			t.Errorf("%s: нет флага --dispute", name)
		}
	}
}

// TestAskCmdHasTitle: all three ask surfaces accept the optional --title, and
// --context says in its help that it is deprecated — the server appends its
// content to the question body (task #1264).
func TestAskCmdHasTitle(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"task ask":      newTaskAskCmd(),
		"task ask-orch": newTaskAskOrchCmd(),
		"agent ask":     newAgentAskCmd(),
	} {
		title := cmd.Flags().Lookup("title")
		if title == nil {
			t.Errorf("%s: нет флага --title", name)
			continue
		}
		if err := cmd.Flags().Parse([]string{"--title", "Короткий заголовок"}); err != nil {
			t.Errorf("%s: --title не разбирается: %v", name, err)
		}
		ctxFlag := cmd.Flags().Lookup("context")
		if ctxFlag == nil {
			t.Errorf("%s: флаг --context пропал, а он должен остаться принимаемым", name)
			continue
		}
		if !strings.Contains(ctxFlag.Usage, "deprecated") {
			t.Errorf("%s: --context не помечен deprecated: %q", name, ctxFlag.Usage)
		}
	}
}

// TestAskRequestBodyTitle: --title rides the request field the API already
// accepts; without it the request carries no "title" key at all, so the
// server derives the heading from the body.
func TestAskRequestBodyTitle(t *testing.T) {
	got := askRequestBody("Короткий заголовок", "тело", "", nil, nil, false)
	want := map[string]any{"title": "Короткий заголовок", "body": "тело"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("askRequestBody = %v, want %v", got, want)
	}

	noTitle := askRequestBody("", "тело", "", nil, nil, false)
	if !reflect.DeepEqual(noTitle, map[string]any{"body": "тело"}) {
		t.Errorf("askRequestBody без --title = %v, want только body", noTitle)
	}

	withContext := askRequestBody("", "тело", "детали", nil, nil, false)
	if withContext["context"] != "детали" {
		t.Errorf("deprecated --context должен уходить в запрос: %v", withContext)
	}
}

// TestRenderWriteResultTitle: a write echoes the resulting heading, so the
// author sees what the dashboard will show — especially when the server
// derived it instead of taking a --title.
func TestRenderWriteResultTitle(t *testing.T) {
	got := renderWriteResult("тред открыт", questionRow{LocalRef: "1264/Q1", Title: "Какой CIDR выставить"})
	if !strings.Contains(got, "Какой CIDR выставить") {
		t.Errorf("result = %q, want the resulting title", got)
	}
	if strings.Contains(renderWriteResult("тред открыт", questionRow{LocalRef: "1264/Q1"}), "заголовок") {
		t.Error("без заголовка строку про заголовок печатать нечего")
	}
	agent := renderWriteResult("тред открыт", agentQuestionRow{LocalRef: "cto/Q1", Title: "Кто владеет решением"})
	if !strings.Contains(agent, "Кто владеет решением") {
		t.Errorf("agent result = %q, want the resulting title", agent)
	}
}
