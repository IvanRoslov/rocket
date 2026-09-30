package cli

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The brief is the plain-language version of a question: what the human would
// otherwise have to go and ask the agent for ("explain it simpler — start with
// the problem, then how the options differ, then what you recommend"). The
// dashboard shows it first and folds the body under «Подробности».
//
// It is required of agents here, in the CLI, rather than in the prompt: the
// prompt scrolls out of a long session's context, a refusal at the moment of
// asking does not. So the refusal text below IS the instruction.

const briefFlagUsage = "вопрос простыми словами для человека, не читавшего сессию: проблема → чем отличаются варианты → рекомендация (обязателен для агентов, кроме --fyi)"

// briefMaxRunes caps the brief so it stays the short version; everything
// else belongs in the body.
const briefMaxRunes = 800

// briefFileToken spots file names and paths — the surest sign a brief is
// written for the agent's own head instead of for the human. A token counts
// when it ends in a source-ish extension and either holds a "/" or starts
// lowercase, so "internal/api/server.go" and "handlers.ts" are caught while
// "Node.js" and "10.0.0.0/16" are not.
var briefFileToken = regexp.MustCompile(`[\w./-]*\.(?:go|ts|tsx|js|jsx|py|rs|sql|md|json|ya?ml|toml|css|sh|swift|kt)\b`)

func briefGuide(example string) string {
	return fmt.Sprintf(`--brief is the plain-language version of your question — the one the human
would otherwise have to come and ask you for: "explain it simpler: start with
the problem in plain words, then how the options differ, then what you
recommend". Write it for someone who has NOT read your session: no file
names, no code, no terms you coined along the way. In the language you use
with the human. Up to %d characters, three short parts:

  **Problem:** what we are deciding and why it matters — 1-3 sentences.
  **Options:** how the options actually differ and what each one leads to.
  **Recommendation:** what you would pick and why — 1-2 sentences.

The body stays for the details (files, logs, code): the dashboard shows the
brief first and folds the body under «Подробности».

  %s --title "<the decision, one line>" \
    --brief "$(cat <<'EOF'
**Problem:** ...

**Options:** ...

**Recommendation:** ...
EOF
)" --file /tmp/q.md --option "<A>" --option "<B>"`, briefMaxRunes, example)
}

// validateBrief enforces the brief on an agent's decision question. fyi notes
// decide nothing and the human asking needs no coaching, so both skip it.
// example is the command prefix the guide's sample starts with.
func validateBrief(brief string, fyi, fromAgent bool, example string) error {
	if fyi || !fromAgent {
		return nil
	}
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return &usageError{message: "a question needs --brief.\n\n" + briefGuide(example)}
	}
	if n := utf8.RuneCountInString(brief); n > briefMaxRunes {
		return &usageError{message: fmt.Sprintf(
			"--brief is %d characters, the limit is %d: it is the short version — move the details into the body.\n\n%s",
			n, briefMaxRunes, briefGuide(example))}
	}
	if strings.Contains(brief, "```") {
		return &usageError{message: "--brief must not contain code blocks — they belong in the body.\n\n" + briefGuide(example)}
	}
	for _, tok := range briefFileToken.FindAllString(brief, -1) {
		if strings.Contains(tok, "/") || startsLower(tok) {
			return &usageError{message: fmt.Sprintf(
				"--brief mentions %q: file names are for the body — say in plain words what that part of the system does.\n\n%s",
				tok, briefGuide(example))}
		}
	}
	return nil
}

func startsLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || (r >= 'a' && r <= 'z')
}

// setBrief adds the brief to an ask request; an empty one is left out so the
// request is unchanged for callers that have none.
func setBrief(req map[string]any, brief string) {
	if b := strings.TrimSpace(brief); b != "" {
		req["brief"] = b
	}
}
