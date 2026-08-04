# CLI local question ids — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development to implement this plan task-by-task.

**Goal:** Let `rocket task reply/answer` and `rocket agent reply/answer` accept a local question reference (`--task 799 Q1`, `799/Q1`) besides the bare global question id.

**Architecture:** A pure parser (`parseQuestionRef`) turns the positional argument plus an optional scope flag into either a global id or a scope+ordinal pair. A resolver (`resolveQuestionRef`) turns the local pair into a global id by listing the scope's questions over the existing API and matching `ordinal` — no daemon/API change. Commands call parse (before connecting) then resolve (after connecting), and keep using the global id in the request path.

**Tech Stack:** Go, cobra, existing `internal/cli` client helpers.

## Global Constraints

- Backward compatibility with bare global ids is mandatory: `rocket task reply 5 "text"` must keep working unchanged.
- No API/daemon changes: reuse `GET /v1/tasks/<id>/questions` and `GET /v1/agents/<role>/questions`.
- Tests live next to the code in `internal/cli/*_test.go`; CLI tests in this package are pure unit tests (no httptest server) — test the parser exhaustively, keep resolution thin.
- Comments and doc-comments in English, user-facing CLI strings in Russian, matching the surrounding files.

---

### Task 1: The reference parser

**Files:**
- Create: `internal/cli/question_ref.go`
- Test: `internal/cli/question_ref_test.go`

**Interfaces:**
- Produces:
  ```go
  type questionRef struct {
      Global  int64  // >0 when the argument was a bare global question id
      Scope   string // task id or role id; empty when Global > 0
      Ordinal int    // 1-based ordinal inside Scope; 0 when Global > 0
  }
  func parseQuestionRef(scope, arg string) (questionRef, error)
  ```
  `err` is a plain error; callers wrap it into `&usageError{}`.

Accepted forms (`scope` is the `--task`/`--role` flag value, may be empty):
- `parseQuestionRef("", "5")` → `{Global: 5}`
- `parseQuestionRef("799", "Q1")` / `"q1"` / `"1"` → `{Scope: "799", Ordinal: 1}` (with a scope flag the positional is always local)
- `parseQuestionRef("", "799/Q1")` / `"799/q1"` / `"799/1"` → `{Scope: "799", Ordinal: 1}`
- `parseQuestionRef("799", "800/Q1")` → error (scope given twice)
- errors: empty arg, `"abc"`, `"Q0"`, `"Q-1"`, `"799/"`, `"/Q1"`, `"a/Q1"` is fine (role scopes are non-numeric — only the ordinal is validated), `"799/Qx"`.

- [ ] **Step 1: Write the failing table test** covering every form above in `question_ref_test.go`, styled like `TestTaskReplyUsage` (table + subtests).
- [ ] **Step 2:** `go test ./internal/cli/ -run QuestionRef` → FAIL (undefined).
- [ ] **Step 3:** Implement `question_ref.go` with the doc comment explaining why the scope flag makes the positional local.
- [ ] **Step 4:** `go test ./internal/cli/ -run QuestionRef` → PASS.
- [ ] **Step 5:** Commit `cli: parse local question references`.

### Task 2: Resolution over the existing list endpoints

**Files:**
- Modify: `internal/cli/question_ref.go`
- Test: `internal/cli/question_ref_test.go`

**Interfaces:**
- Consumes: `questionRef` from Task 1, `fetchQuestions(c, taskID, openOnly)` (`internal/cli/task.go:740`), `agentQuestionRow`.
- Produces:
  ```go
  // pickOrdinal finds the id of the question with the given ordinal.
  func pickOrdinal(ords []int, ids []int64, ref questionRef) (int64, error)
  ```
  plus `resolveTaskQuestion(c *client.Client, ref questionRef) (string, error)` and
  `resolveAgentQuestion(c *client.Client, ref questionRef) (string, error)`, each returning the
  global id as a string ready for `apiPath`, and returning `ref.Global` verbatim when set (no HTTP call).

- [ ] **Step 1:** Failing test for `pickOrdinal`: found, not found (`Q7` in a 2-question scope → error naming the scope and ordinal), and global passthrough.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** Implement; error text Russian: `вопрос Q%d не найден в %s`.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5:** Commit `cli: resolve local question references to global ids`.

### Task 3: Wire `rocket task reply` and `rocket task answer`

**Files:**
- Modify: `internal/cli/task.go` (`newTaskReplyCmd` ~832, `newTaskAnswerCmd` ~869)
- Test: `internal/cli/task_test.go`

- [ ] **Step 1:** Extend `TestTaskReplyUsage`/`TestTaskAnswerUsage`: `--task 799` with `Q0` errors; `not-a-number` still errors; and add a case asserting `Q1` **without** `--task` and without a `<id>/` prefix errors as a usage error.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** Add `--task` string flag; replace the `strconv.ParseInt(args[0]...)` guard with `parseQuestionRef(taskFlag, args[0])` wrapped in `usageError`; after `connect`, `resolveTaskQuestion` and use the resulting id in `apiPath`. Update `Use:`/usage strings to `<question-id>|<task>/Q<n>` and mention `--task`.
- [ ] **Step 4:** run `go test ./internal/cli/` → PASS.
- [ ] **Step 5:** Commit `cli: task reply/answer accept local question refs`.

### Task 4: Wire `rocket agent reply` (and `agent answer` for symmetry)

**Files:**
- Modify: `internal/cli/agent_questions.go` (`newAgentReplyCmd` ~127, `newAgentAnswerCmd` ~168)
- Test: `internal/cli/agent_questions_test.go`

Scope flag is `--role`; `<role>/Q1` is the inline form; a bare `Q1` resolves the role from the
current session via `resolveAgentID("")` — pending orchestrator confirmation, see the log entry.

- [ ] **Step 1:** Failing usage tests mirroring Task 3.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3:** Implement with `resolveAgentQuestion`.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5:** Commit `cli: agent reply/answer accept local question refs`.

### Task 5: Docs and verification

**Files:**
- Modify: `docs/04-cli.md`

- [ ] **Step 1:** Document both forms next to the question commands.
- [ ] **Step 2:** `go build ./... && go test ./... && gofmt -l internal cmd`.
- [ ] **Step 3:** End-to-end smoke against a running daemon if available.
- [ ] **Step 4:** Commit + PR.
