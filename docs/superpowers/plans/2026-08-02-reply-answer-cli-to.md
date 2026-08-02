# CLI `--to` and participants output Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the participant model of question threads in the `rocket` CLI: an
optional `--to` flag on every thread-writing command, and participants,
waiting-on and your-turn in every thread-reading command.

**Architecture:** The API (subtask #731) already accepts `"to": [...]` on all six
thread-writing endpoints and already returns `participants`, `waiting_on`,
`your_turn` and `messages[].addressed_to`. This change is CLI-only: two shared
pure helpers (`parseTo`, `setTo`) that every writing command uses to build the
request body, four new fields on the JSON row structs so `--json` carries the
new contract through, and a reworked turn arrow plus a participants line in the
two render functions. No new API calls, no client-package changes —
`client.Post` already takes an arbitrary body.

**Tech Stack:** Go, cobra, `internal/cli` unit tests (`go test ./internal/cli/...`).

## Global Constraints

- Code, identifiers and comments in English; user-facing strings in Russian,
  matching the surrounding CLI output.
- Absent `--to` must produce a byte-identical request body to today: no `to`
  key at all, not an empty array.
- The author wire is still legacy: `messages[].author` and `asked_by` are `""`
  for the human, while `participants`/`waiting_on`/`addressed_to` use the
  canonical `human`. Treat BOTH `""` and `"human"` as the human everywhere.
- `to` decides who must RESPOND (`waiting_on`), never who is NOTIFIED.
- Repo has no CI; local `go build ./... && go vet ./... && gofmt -l . && go test ./...`
  is the gate. Known pre-existing failures to ignore:
  `internal/cli.TestLoadConfigNoOverrideWhenSocketFlagEmpty`,
  `internal/queue.TestQueue_TimeoutExpiryAlsoNotifiesSender`,
  `internal/session.TestAnswerQuiz_SecondCallWhileInFlightIs409ThenClearsAfterResolved`.

---

## File Structure

- `internal/cli/task.go` — task-thread commands (`ask`, `ask-orch`, `reply`,
  `answer`, `questions`), `questionRow`/`questionMessageRow`, `renderQuestions`.
  Also gains the two shared `--to` helpers, since it is the file both thread
  surfaces already borrow `questionMessageRow` from.
- `internal/cli/agent_questions.go` — role-thread commands (`agent ask`,
  `agent reply`, `agent answer`, `agent questions`), `agentQuestionRow`,
  `renderAgentQuestions`.
- `internal/cli/task_test.go`, `internal/cli/agent_questions_test.go` — tests.

---

### Task 1: `--to` on every thread-writing command

**Files:**
- Modify: `internal/cli/task.go` (`newTaskAskCmd`, `newTaskAskOrchCmd`,
  `newTaskReplyCmd`, `newTaskAnswerCmd`; new helpers `parseTo`, `setTo`)
- Modify: `internal/cli/agent_questions.go` (`newAgentAskCmd`,
  `newAgentReplyCmd`, `newAgentAnswerCmd`)
- Test: `internal/cli/task_test.go`, `internal/cli/agent_questions_test.go`

**Interfaces:**
- Produces:
  - `func parseTo(vals []string) []string` — flattens comma-separated values,
    trims spaces, drops empties, dedupes preserving first-seen order; returns
    `nil` for no usable ids.
  - `func setTo(reqBody map[string]any, to []string)` — sets `reqBody["to"]`
    only when `len(to) > 0`.

- [ ] **Step 1: Write the failing tests for `parseTo` and `setTo`**

```go
// TestParseTo covers the --to normalisation: comma splitting, repetition,
// trimming, empty segments and deduplication.
func TestParseTo(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empty string", []string{""}, nil},
		{"single", []string{"cto"}, []string{"cto"}},
		{"comma separated", []string{"cto,human"}, []string{"cto", "human"}},
		{"repeated flag", []string{"cto", "human"}, []string{"cto", "human"}},
		{"spaces trimmed", []string{" cto , human "}, []string{"cto", "human"}},
		{"empty segments dropped", []string{"cto,,"}, []string{"cto"}},
		{"deduped", []string{"cto", "cto,human"}, []string{"cto", "human"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTo(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("parseTo(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseTo(%v) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}

// TestSetToOmitsEmpty tests that no --to leaves the request body byte-identical
// to before this change: no "to" key at all, not an empty array.
func TestSetToOmitsEmpty(t *testing.T) {
	body := map[string]any{"body": "q"}
	setTo(body, nil)
	if _, ok := body["to"]; ok {
		t.Errorf("expected no \"to\" key for empty to, got %v", body)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"body":"q"}` {
		t.Errorf("expected unchanged body JSON, got %s", raw)
	}
}

// TestSetToAddsRecipients tests that --to reaches the request body as an array.
func TestSetToAddsRecipients(t *testing.T) {
	body := map[string]any{"body": "q"}
	setTo(body, []string{"cto", "human"})
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"body":"q","to":["cto","human"]}` {
		t.Errorf("unexpected body JSON: %s", raw)
	}
}

// TestTaskThreadCommandsHaveToFlag tests that every task thread-writing
// command registers --to.
func TestTaskThreadCommandsHaveToFlag(t *testing.T) {
	cmds := map[string]*cobra.Command{
		"ask":       newTaskAskCmd(),
		"ask-orch":  newTaskAskOrchCmd(),
		"reply":     newTaskReplyCmd(),
		"answer":    newTaskAnswerCmd(),
	}
	for name, cmd := range cmds {
		if cmd.Flags().Lookup("to") == nil {
			t.Errorf("task %s: expected --to flag", name)
		}
	}
}
```

And in `agent_questions_test.go`:

```go
// TestAgentThreadCommandsHaveToFlag tests that every role thread-writing
// command registers --to.
func TestAgentThreadCommandsHaveToFlag(t *testing.T) {
	cmds := map[string]*cobra.Command{
		"ask":    newAgentAskCmd(),
		"reply":  newAgentReplyCmd(),
		"answer": newAgentAnswerCmd(),
	}
	for name, cmd := range cmds {
		if cmd.Flags().Lookup("to") == nil {
			t.Errorf("agent %s: expected --to flag", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestParseTo|TestSetTo|ThreadCommandsHaveToFlag' -v`
Expected: FAIL — `undefined: parseTo`, `undefined: setTo`.

- [ ] **Step 3: Implement the helpers and wire the flag**

In `internal/cli/task.go`:

```go
// parseTo normalises --to values into participant ids. The flag is both
// comma-separated and repeatable, so "--to a,b --to c" and "--to a --to b,c"
// mean the same thing. Blank segments are dropped and duplicates collapse,
// preserving first-seen order so the request body is predictable.
func parseTo(vals []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

// setTo attaches the addressees to a thread-write request body. No --to means
// no "to" key at all rather than an empty array, which keeps the request
// byte-identical to what the CLI sent before the participant model existed.
func setTo(reqBody map[string]any, to []string) {
	if len(to) > 0 {
		reqBody["to"] = to
	}
}
```

Then in each of the seven writing commands add:

```go
var to []string
// ...
cmd.Flags().StringSliceVar(&to, "to", nil, "кому адресован вопрос (id участников через запятую)")
```

and after the request body is built:

```go
setTo(reqBody, parseTo(to))
```

`newTaskAnswerCmd` and `newAgentAnswerCmd` build `reqBody` in both the
`--dismiss` and the body branch; call `setTo` once after that if/else.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestParseTo|TestSetTo|ThreadCommandsHaveToFlag' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/task.go internal/cli/agent_questions.go internal/cli/task_test.go internal/cli/agent_questions_test.go
git commit -m "cli: --to on task and role thread writes"
```

---

### Task 2: participants and waiting-on in thread output

**Files:**
- Modify: `internal/cli/task.go` (`questionMessageRow`, `questionRow`,
  `renderQuestions`)
- Modify: `internal/cli/agent_questions.go` (`agentQuestionRow`,
  `renderAgentQuestions`)
- Test: `internal/cli/task_test.go`, `internal/cli/agent_questions_test.go`

**Format decision (from the orchestrator, message 8143):** the whose_turn arrow
is REPLACED by a waiting_on arrow of the same shape and language — one
statement of whose turn it is, never two that can disagree. The old
`" → ждёт ответа пользователя"` / `" → ждёт оркестратора"` strings go away; the
`whose_turn` JSON field stays on the struct for `--json` consumers and is
simply no longer printed (it is retired in subtask #736, not here).

Target output:

```
task #42
Q1 (#10) [open] → ждут: cto, human (ваш ход)
  Q body
  context: extra
  участники: human, reply-answer-orch, cto
  [user] human reply text
  [cto → reply-answer-orch] targeted reply
```

**Interfaces:**
- Consumes: `parseTo`/`setTo` from Task 1 (unchanged here).
- Produces:
  - `questionMessageRow.AddressedTo []string` (`json:"addressed_to,omitempty"`)
  - `questionRow` / `agentQuestionRow` gain
    `Participants []string` (`json:"participants,omitempty"`),
    `WaitingOn []string` (`json:"waiting_on,omitempty"`),
    `YourTurn bool` (`json:"your_turn,omitempty"`)
  - `func threadTurnArrow(waiting []string, yourTurn bool) string`
  - `func threadAuthorLabel(author string) string` — `""` and `"human"` both
    render as `user`.
  - `func renderThreadMessage(sb *strings.Builder, m questionMessageRow)`

- [ ] **Step 1: Write the failing tests**

In `task_test.go` (and replace the two existing whose_turn arrow tests,
`TestRenderQuestionsWhoseTurnUser` and
`TestRenderQuestionsWhoseTurnOrchestrator`, which assert the retired strings):

```go
// TestRenderQuestionsWaitingArrow tests that the header arrow names who is
// awaited, replacing the pre-participant whose_turn arrow.
func TestRenderQuestionsWaitingArrow(t *testing.T) {
	qs := []questionRow{{
		ID: 5, Ordinal: 1, Status: "open", WhoseTurn: "user", Body: "Which approach?",
		Participants: []string{"human", "cto"},
		WaitingOn:    []string{"cto", "human"},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "Q1 (#5) [open] → ждут: cto, human") {
		t.Errorf("expected waiting-on arrow, got: %q", out)
	}
	if strings.Contains(out, "ждёт ответа пользователя") || strings.Contains(out, "ждёт оркестратора") {
		t.Errorf("expected the whose_turn arrow to be gone, got: %q", out)
	}
	if !strings.Contains(out, "  участники: human, cto") {
		t.Errorf("expected participants line, got: %q", out)
	}
}

// TestRenderQuestionsNoWaitingNoArrow tests that a thread nobody is awaited on
// — a resolved one, or a pre-participant server — renders no arrow.
func TestRenderQuestionsNoWaitingNoArrow(t *testing.T) {
	qs := []questionRow{
		{ID: 7, Ordinal: 3, Status: "resolved", WhoseTurn: "", Body: "Done question"},
	}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "Q3 (#7) [resolved]") {
		t.Errorf("expected resolved header, got: %q", out)
	}
	if strings.Contains(out, "ждут") || strings.Contains(out, "участники") {
		t.Errorf("expected no arrow and no participants line, got: %q", out)
	}
}

// TestRenderQuestionsYourTurn tests that a thread waiting on the caller is
// marked, so "rocket task questions" shows what needs an answer.
func TestRenderQuestionsYourTurn(t *testing.T) {
	qs := []questionRow{{
		ID: 12, Ordinal: 1, Status: "open", Body: "Q body",
		Participants: []string{"human", "cto"},
		WaitingOn:    []string{"human"},
		YourTurn:     true,
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "→ ждут: human (ваш ход)") {
		t.Errorf("expected your-turn marker, got: %q", out)
	}
}

// TestRenderQuestionsCanonicalHumanAuthor tests that the canonical "human"
// author renders like the legacy empty author: the wire still sends "" today,
// but subtask #736 flips it and the CLI must read both.
func TestRenderQuestionsCanonicalHumanAuthor(t *testing.T) {
	qs := []questionRow{{
		ID: 13, Ordinal: 1, Status: "open", Body: "Q body",
		Messages: []questionMessageRow{
			{ID: 1, Author: "human", Kind: "reply", Body: "canonical human"},
			{ID: 2, Author: "", Kind: "reply", Body: "legacy human"},
		},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "[user] canonical human") {
		t.Errorf("expected canonical human rendered as user, got: %q", out)
	}
	if !strings.Contains(out, "[user] legacy human") {
		t.Errorf("expected legacy human rendered as user, got: %q", out)
	}
}

// TestRenderQuestionsAddressedTo tests that a targeted message names its
// addressees in the frame.
func TestRenderQuestionsAddressedTo(t *testing.T) {
	qs := []questionRow{{
		ID: 14, Ordinal: 1, Status: "open", Body: "Q body",
		Messages: []questionMessageRow{
			{ID: 1, Author: "cto", Kind: "reply", Body: "targeted",
				AddressedTo: []string{"reply-answer-orch", "human"}},
		},
	}}
	out := renderQuestions(42, qs)
	if !strings.Contains(out, "[cto → reply-answer-orch, human] targeted") {
		t.Errorf("expected addressed-to frame, got: %q", out)
	}
}
```

The same five tests, adapted, in `agent_questions_test.go` against
`renderAgentQuestions("cto", qs)` with `agentQuestionRow`, replacing any
existing whose_turn arrow assertions there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'RenderQuestions|RenderAgentQuestions' -v`
Expected: FAIL — unknown fields `Participants`, `WaitingOn`, `YourTurn`,
`AddressedTo`.

- [ ] **Step 3: Implement**

Add to `questionMessageRow`:

```go
	// AddressedTo narrows who is expected to respond. Empty means every
	// participant except the author.
	AddressedTo []string `json:"addressed_to,omitempty"`
```

Add to both `questionRow` and `agentQuestionRow` — alongside `WhoseTurn`, which
stays on the wire for web and mobile even though the CLI stops printing it:

```go
	// Participants is everyone taking part in the thread; WaitingOn is the
	// subset expected to speak next; YourTurn says whether this CLI's caller
	// is one of them.
	Participants []string `json:"participants,omitempty"`
	WaitingOn    []string `json:"waiting_on,omitempty"`
	YourTurn     bool     `json:"your_turn,omitempty"`
```

Shared render helpers in `task.go`, used by both renderers:

```go
// threadTurnArrow renders the header suffix naming who is expected to speak
// next. It replaces the pre-participant whose_turn arrow: with several
// participants "the orchestrator" is no longer a thing a single word can name.
// Nobody awaited — a resolved thread, or a server that predates the
// participant model — renders nothing, as the old arrow did.
func threadTurnArrow(waiting []string, yourTurn bool) string {
	if len(waiting) == 0 {
		return ""
	}
	arrow := " → ждут: " + strings.Join(waiting, ", ")
	if yourTurn {
		arrow += " (ваш ход)"
	}
	return arrow
}

// threadAuthorLabel renders a message author for a thread line. The human is
// spelled "" on the wire today and "human" after subtask #736 flips it; both
// render as "user", the word the CLI has always shown.
func threadAuthorLabel(author string) string {
	if author == "" || author == "human" {
		return "user"
	}
	return author
}

// renderThreadMessage writes one thread line: "  [author] body", or
// "  [author → a, b] body" when the message is addressed at somebody in
// particular.
func renderThreadMessage(sb *strings.Builder, m questionMessageRow) {
	frame := threadAuthorLabel(m.Author)
	if len(m.AddressedTo) > 0 {
		frame += " → " + strings.Join(m.AddressedTo, ", ")
	}
	fmt.Fprintf(sb, "  [%s] %s\n", frame, m.Body)
}

// renderParticipantsLine writes the thread's participant line, omitted when
// the server sent none.
func renderParticipantsLine(sb *strings.Builder, participants []string) {
	if len(participants) > 0 {
		fmt.Fprintf(sb, "  участники: %s\n", strings.Join(participants, ", "))
	}
}
```

In `renderQuestions` and `renderAgentQuestions`: drop the `switch q.WhoseTurn`
block in favour of `threadTurnArrow(q.WaitingOn, q.YourTurn)`, call
`renderParticipantsLine` after the context line, and replace the inline
author fallback loop body with `renderThreadMessage(&sb, m)`.

`agentQuestionRow.Messages` is already `[]questionMessageRow`, so both
renderers share `renderThreadMessage` unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/...`
Expected: PASS apart from the known pre-existing
`TestLoadConfigNoOverrideWhenSocketFlagEmpty`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/task.go internal/cli/agent_questions.go internal/cli/task_test.go internal/cli/agent_questions_test.go
git commit -m "cli: show thread participants and who is awaited"
```

---


### Task 3: verification and PR

- [ ] **Step 1: Confirm no CLI-side answer guard remains**

Run: `grep -n "ROCKET_SESSION_ID" internal/cli/task.go internal/cli/agent_questions.go`
Expected: only `newTaskAskCmd`'s ask-direction guard, which is about `ask` vs
`ask-orch`, not about who may answer. If an answer-side human-only guard turns
up, remove it (brief item 5); otherwise report that item as a no-op.

- [ ] **Step 2: Full verification**

```bash
gofmt -l . | grep -v node_modules
go build ./...
go vet ./...
go test ./...
```

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feature/reply-answer/cli-to
gh pr create --title "cli: --to flag and participant output on question threads" --body "..."
```
