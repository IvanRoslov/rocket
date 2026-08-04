package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// The CLI prints threads as "Q1 (#372)": Q1 is the ordinal inside the task (or
// role), #372 the global question id. Until now only the global id could be
// typed back, and replies regularly landed in the wrong thread because the two
// numbers sit next to each other. questionRef lets a command take either.
//
// A reference is one of:
//   - global: "372" — passed through to the API path untouched;
//   - local: an ordinal plus the scope (task id or role) it counts inside,
//     spelled "--task 799 Q1" or "799/Q1" (the Q is optional and case-insensitive).
type questionRef struct {
	// Global is the global question id as typed; empty for a local reference.
	Global string
	// Scope is the task id or role id the ordinal counts inside.
	Scope string
	// Ordinal is the 1-based per-scope number shown as Q<n>.
	Ordinal int
}

// taskFlagUsage and agentFlagUsage document the scope flags on the thread
// commands. The agent dimension is spelled --agent everywhere in the CLI
// (rocket inbox --agent), so it is spelled that way here too.
const taskFlagUsage = "задача, внутри которой считается Q<n> (для локальной ссылки вида Q1)"
const agentFlagUsage = "агент, внутри которого считается Q<n> (для локальной ссылки вида Q1)"

// agentRefUsage lists the explicit agent-scoped forms, shown when the agent
// cannot be inferred from the calling session.
const agentRefUsage = `укажите агента: --agent <role> Q<n> или <role>/Q<n> ` +
	`(например: --agent sre Q1 или sre/Q1)`

// refUsage lists both local forms. A user who typed "Q1" without a scope
// learns them from nowhere else.
const refUsage = `укажите задачу: --task <task-id> Q<n> или <task-id>/Q<n> ` +
	`(например: --task 799 Q1 или 799/Q1)`

// parseQuestionRef parses a question reference. scope is the value of the
// command's scope flag ("" when unset); when it is set the positional argument
// is always local — a bare "2" then means Q2, not question #2. An inline
// "799/Q1" carries its own scope and may not be combined with the flag.
func parseQuestionRef(arg, scope string) (questionRef, error) {
	if arg == "" {
		return questionRef{}, &usageError{message: "invalid question id"}
	}

	if inline, rest, ok := strings.Cut(arg, "/"); ok {
		if scope != "" {
			return questionRef{}, &usageError{
				message: "задача указана дважды: и в --task, и в " + arg}
		}
		if inline == "" {
			return questionRef{}, &usageError{message: "invalid question id: " + arg}
		}
		ord, err := parseOrdinal(rest)
		if err != nil {
			return questionRef{}, err
		}
		return questionRef{Scope: inline, Ordinal: ord}, nil
	}

	if scope != "" {
		ord, err := parseOrdinal(arg)
		if err != nil {
			return questionRef{}, err
		}
		return questionRef{Scope: scope, Ordinal: ord}, nil
	}

	// No scope anywhere: only a global id is meaningful. A "Q1" here is not an
	// invalid id but a missing scope, and says so.
	if _, err := strconv.ParseInt(arg, 10, 64); err != nil {
		if isOrdinalLike(arg) {
			return questionRef{}, &usageError{message: refUsage}
		}
		return questionRef{}, &usageError{message: "invalid question id"}
	}
	return questionRef{Global: arg}, nil
}

// parseOrdinal parses "Q1", "q1" or "1" into 1. Ordinals start at 1.
func parseOrdinal(s string) (int, error) {
	digits := strings.TrimPrefix(strings.TrimPrefix(s, "Q"), "q")
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, &usageError{message: "invalid question ordinal: " + s}
	}
	return n, nil
}

// isOrdinalLike reports whether s looks like a bare ordinal ("Q1"), i.e. the
// user meant a local reference but named no scope.
func isOrdinalLike(s string) bool {
	_, err := parseOrdinal(s)
	return err == nil
}

// pickOrdinal maps ordinal → global id for one scope. scopeName names the
// scope in Russian for the error ("задачи #799", "агента sre").
func pickOrdinal(ids map[int]int64, ref questionRef, scopeName string) (string, error) {
	id, ok := ids[ref.Ordinal]
	if !ok {
		return "", fmt.Errorf("у %s нет вопроса Q%d", scopeName, ref.Ordinal)
	}
	return strconv.FormatInt(id, 10), nil
}

// resolveQuestionRef turns a user-supplied question reference into the global
// id the API path needs. A bare number passes through unchanged; a local
// reference is resolved against the task's threads, whose ordinals the daemon
// already computes. taskFlag is 0 when --task was not given.
func resolveQuestionRef(arg string, taskFlag int64) (globalID string, err error) {
	scope := ""
	if taskFlag != 0 {
		scope = strconv.FormatInt(taskFlag, 10)
	}
	ref, err := parseQuestionRef(arg, scope)
	if err != nil {
		return "", err
	}
	if ref.Global != "" {
		return ref.Global, nil
	}

	c, _, err := connect(true)
	if err != nil {
		return "", err
	}
	qs, err := fetchQuestions(c, ref.Scope, false)
	if err != nil {
		return "", err
	}
	ids := make(map[int]int64, len(qs))
	for _, q := range qs {
		ids[q.Ordinal] = q.ID
	}
	return pickOrdinal(ids, ref, "задачи #"+ref.Scope)
}

// resolveAgentQuestionRef is resolveQuestionRef for role threads, whose
// ordinals count inside an agent rather than a task. The scope comes from
// --agent, from an inline "sre/Q1", or — for a bare "Q1" — from the calling
// session, the same default "rocket agent questions" uses. agentFlag is ""
// when --agent was not given.
func resolveAgentQuestionRef(arg, agentFlag string) (globalID string, err error) {
	ref, err := parseQuestionRef(arg, agentFlag)
	if err != nil {
		if agentFlag == "" && ordinalWithoutScope(arg) {
			return resolveAgentOrdinalInSession(arg)
		}
		return "", err
	}
	if ref.Global != "" {
		return ref.Global, nil
	}
	return resolveAgentOrdinal(ref)
}

// ordinalWithoutScope reports whether arg is a bare ordinal — the one case
// where the role comes from the session instead of the argument.
func ordinalWithoutScope(arg string) bool {
	return !strings.Contains(arg, "/") && isOrdinalLike(arg) &&
		!isGlobalID(arg)
}

func isGlobalID(arg string) bool {
	_, err := strconv.ParseInt(arg, 10, 64)
	return err == nil
}

func resolveAgentOrdinalInSession(arg string) (string, error) {
	role, err := resolveAgentID("")
	if err != nil {
		// Outside an agent session there is nothing to guess from: name the
		// agent explicitly, either way.
		return "", &usageError{message: agentRefUsage}
	}
	ord, err := parseOrdinal(arg)
	if err != nil {
		return "", err
	}
	return resolveAgentOrdinal(questionRef{Scope: role, Ordinal: ord})
}

func resolveAgentOrdinal(ref questionRef) (string, error) {
	c, _, err := connect(true)
	if err != nil {
		return "", err
	}
	var resp struct {
		Questions []agentQuestionRow `json:"questions"`
	}
	if err := c.Get(apiPath("v1", "agents", ref.Scope, "questions"), nil, &resp); err != nil {
		return "", err
	}
	ids := make(map[int]int64, len(resp.Questions))
	for _, q := range resp.Questions {
		ids[q.Ordinal] = q.ID
	}
	return pickOrdinal(ids, ref, "агента "+ref.Scope)
}
