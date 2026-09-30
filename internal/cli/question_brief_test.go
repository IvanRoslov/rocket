package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const goodBrief = "**Проблема:** staging и prod сидят в одной сети, и падение одного задевает другой.\n\n" +
	"**Варианты:** развести сети — день работы, зато изоляция; оставить — ничего не делаем, риск остаётся.\n\n" +
	"**Рекомендация:** развести, 10.0.0.0/16 свободна."

func TestValidateBrief_RequiredFromAgents(t *testing.T) {
	err := validateBrief("", false, true, "rocket task ask 12")
	if err == nil {
		t.Fatal("an agent's decision question without --brief must be rejected")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected usageError, got %T", err)
	}
	// The refusal IS the instruction: it must carry the whole template, so an
	// agent deep in a long session learns how to write the brief right here.
	for _, want := range []string{"--brief", "Problem", "Options", "Recommendation", "rocket task ask 12"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must mention %q, got:\n%s", want, err)
		}
	}
}

func TestValidateBrief_NotRequired(t *testing.T) {
	if err := validateBrief("", true, true, "x"); err != nil {
		t.Errorf("--fyi is a status note with nothing to decide, brief must be optional: %v", err)
	}
	if err := validateBrief("", false, false, "x"); err != nil {
		t.Errorf("the human asking a role needs no brief: %v", err)
	}
}

func TestValidateBrief_Accepts(t *testing.T) {
	if err := validateBrief(goodBrief, false, true, "x"); err != nil {
		t.Errorf("a plain brief with a CIDR in it must pass: %v", err)
	}
}

func TestValidateBrief_TooLong(t *testing.T) {
	long := strings.Repeat("слово ", briefMaxRunes/6+10)
	err := validateBrief(long, false, true, "x")
	if err == nil || !strings.Contains(err.Error(), "800") {
		t.Fatalf("an over-long brief must be rejected naming the limit, got %v", err)
	}
}

func TestValidateBrief_Jargon(t *testing.T) {
	for name, brief := range map[string]string{
		"code block": "**Проблема:** падает так:\n```\npanic: nil map\n```",
		"file path":  "**Проблема:** internal/api/server.go не регистрирует маршрут.",
		"bare file":  "**Проблема:** в handlers.ts нет мока.",
	} {
		if err := validateBrief(brief, false, true, "x"); err == nil {
			t.Errorf("%s: brief must be rejected as jargon", name)
		}
	}
}

func TestAskCmdsHaveBrief(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"task ask":  newTaskAskCmd(),
		"agent ask": newAgentAskCmd(),
	} {
		if cmd.Flags().Lookup("brief") == nil {
			t.Errorf("expected --brief on %s", name)
		}
	}
}

// TestTaskAskRefusesWithoutBrief: the refusal comes before any daemon call,
// so it fires even with no daemon running.
func TestTaskAskRefusesWithoutBrief(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "orch-1")
	cmd := newTaskAskCmd()
	cmd.SetArgs([]string{"12", "--title", "Какой CIDR?", "вопрос"})
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) || !strings.Contains(err.Error(), "--brief") {
		t.Fatalf("expected a --brief usage refusal, got %v", err)
	}
}

func TestSetBrief(t *testing.T) {
	req := map[string]any{"body": "b"}
	setBrief(req, "")
	if _, ok := req["brief"]; ok {
		t.Error("an empty brief must not be sent")
	}
	setBrief(req, "кратко")
	if req["brief"] != "кратко" {
		t.Errorf("brief = %v", req["brief"])
	}
}
