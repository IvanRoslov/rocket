package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestValidateBrainstormFlags: a brainstorm question with options must name
// the recommended one, and the flags must not contradict each other — all
// caught before the daemon is called.
func TestValidateBrainstormFlags(t *testing.T) {
	tests := []struct {
		name       string
		brainstorm bool
		fyi        bool
		options    []string
		recommend  int
		wantErr    string
	}{
		{"brainstorm с рекомендацией", true, false, []string{"A", "B"}, 2, ""},
		{"brainstorm без вариантов", true, false, nil, 0, "--option"},
		{"brainstorm с одним вариантом", true, false, []string{"A"}, 1, "--option"},
		{"обычный вопрос", false, false, []string{"A", "B"}, 0, ""},
		{"нет --recommend", true, false, []string{"A", "B"}, 0, "--recommend"},
		{"--recommend вне диапазона", true, false, []string{"A", "B"}, 3, "--recommend"},
		{"--recommend без --brainstorm", false, false, []string{"A"}, 1, "--brainstorm"},
		{"--brainstorm с --fyi", true, true, nil, 0, "--fyi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrainstormFlags(tt.brainstorm, tt.fyi, tt.options, tt.recommend, "usage line")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want a usage error mentioning %s", err, tt.wantErr)
			}
		})
	}
}

func TestSetBrainstorm(t *testing.T) {
	req := askRequestBody("", "Какую схему?", "", nil, []string{"A", "B"}, false)
	setBrainstorm(req, true, 2)
	want := map[string]any{"body": "Какую схему?", "options": []string{"A", "B"}, "type": "brainstorm", "recommend": 2}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("request = %v, want %v", req, want)
	}

	plain := askRequestBody("", "вопрос?", "", nil, nil, false)
	setBrainstorm(plain, false, 0)
	if !reflect.DeepEqual(plain, map[string]any{"body": "вопрос?"}) {
		t.Errorf("request without --brainstorm = %v, want unchanged", plain)
	}
}

func TestTaskAskHasBrainstormFlags(t *testing.T) {
	cmd := newTaskAskCmd()
	for _, flag := range []string{"brainstorm", "recommend"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s on task ask", flag)
		}
	}
}

// TestTaskAskBrainstormRefusesWithoutRecommend: the refusal comes before any
// daemon call, so it fires with no daemon running.
func TestTaskAskBrainstormRefusesWithoutRecommend(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "orch-1")
	cmd := newTaskAskCmd()
	cmd.SetArgs([]string{"12", "--brainstorm", "--brief", "Проблема простыми словами.",
		"--option", "A", "--option", "B", "Какую схему?"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "--recommend") {
		t.Fatalf("expected a --recommend usage refusal, got %v", err)
	}
}

func TestRecordRequestBody(t *testing.T) {
	tests := []struct {
		name string
		opts brainstormRecordOptions
		want map[string]any
	}{
		{"выбор", brainstormRecordOptions{choose: 2}, map[string]any{"choose": 2}},
		{"выбор с комментарием", brainstormRecordOptions{choose: 1, body: "но без кэша"}, map[string]any{"choose": 1, "body": "но без кэша"}},
		{"свой текст", brainstormRecordOptions{body: "давай про деньги"}, map[string]any{"body": "давай про деньги"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.requestBody(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("requestBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordOptionsValidate(t *testing.T) {
	for name, tt := range map[string]struct {
		opts    brainstormRecordOptions
		wantErr bool
	}{
		"выбор":         {brainstormRecordOptions{choose: 1}, false},
		"текст":         {brainstormRecordOptions{body: "x"}, false},
		"ничего":        {brainstormRecordOptions{}, true},
		"отрицательный": {brainstormRecordOptions{choose: -1, body: "x"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tt.opts.validate("usage"); tt.wantErr != (err != nil) {
				t.Fatalf("validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTaskBrainstormRecordRegistered(t *testing.T) {
	var group *cobra.Command
	for _, c := range newTaskCmd().Commands() {
		if c.Name() == "brainstorm" {
			group = c
		}
	}
	if group == nil {
		t.Fatal("expected `task brainstorm`")
	}
	var record *cobra.Command
	for _, c := range group.Commands() {
		if c.Name() == "record" {
			record = c
		}
	}
	if record == nil {
		t.Fatal("expected `task brainstorm record`")
	}
	for _, flag := range []string{"choose", "file", "task"} {
		if record.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s on task brainstorm record", flag)
		}
	}
}

func TestTaskBrainstormRecordUsageError(t *testing.T) {
	cmd := newTaskBrainstormRecordCmd()
	cmd.SetArgs([]string{"12/Q1"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var usageErr *usageError
	if err := cmd.Execute(); !errors.As(err, &usageErr) {
		t.Fatalf("expected a usage error with neither --choose nor text, got %v", err)
	}
}
