# T1 — Unified question threads and participants (store layer)

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:test-driven-development`
> for every task below. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Collapse the two near-identical Q&A thread schemas — `task_questions` /
`question_messages` and `agent_questions` / `agent_question_messages` — into one
`questions` / `question_messages` pair with an explicit `question_participants`
table, migrating every existing row without losing history.

**Architecture:** A single migration `0009_threads.sql` rebuilds the task-thread
tables in place (task thread ids are preserved verbatim) and copies the role
threads in at a fixed id offset, so message→thread references stay correct and
every ordinal is unchanged. `internal/store/questions.go` becomes the one DAO;
`internal/store/agent_questions.go` is reduced to a thin facade over it with
`role_id` set, so `internal/api/agent_questions.go` keeps compiling unchanged.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), embedded SQL migrations in
`internal/store/migrations`.

## Global Constraints

- Spec `spec v1` of task #722 is the source of truth; a divergence is a question
  for the orchestrator, not a local decision.
- Scope is `internal/store`, plus the minimum outside it needed to keep
  `go build ./...` and `go test ./...` green.
- Code, identifiers, comments and commit messages in English.
- Backward compatibility is a hard requirement: no existing thread, message or
  ordinal may be lost or renumbered in a way that breaks its thread.
- Branch `feature/reply-answer/store-threads`, one PR.

## Participant identifiers

| Value | Who |
|---|---|
| `human` | the human user |
| persistent agent id (`cto`) | a `kind=agent` session |
| session id (`reply-answer-orch`) | an orchestrator or worker |

Legacy rows store the human as `author = ''` / `asked_by = ''`. The migration
**does** rewrite `question_messages.author` (including the rows moved in from
`agent_question_messages`) to `human`, so a dual representation does not live in
the table forever. Readers and aggregate SQL stay tolerant of `''` anyway, as
cheap defence.

Rule of thumb (orchestrator, confirming the brief): participant-id columns —
`question_participants.participant_id`, `question_messages.author`,
`question_messages.addressed_to` — always carry canonical ids and never `''`.
Everything else keeps its current shape; in particular `questions.asked_by`
stays `''` for the human, because it is not a participant-id column and
deriving the first turn from it is T2's job.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/migrations/0009_threads.sql` (create) | table rebuild, role-thread data migration, participant backfill |
| `internal/store/questions.go` (modify) | the single thread DAO: questions, messages, participants |
| `internal/store/questions_reopen.go` (modify) | `ReopenQuestion` against `questions` |
| `internal/store/agent_questions.go` (modify) | thin facade over the same tables with `role_id` set |
| `internal/store/agents.go:107-135` (modify) | role deletion cascades into `questions` / `question_messages` / `question_participants` |
| `internal/api/questions.go:68`, `internal/api/agent_questions.go:47`, `internal/heartbeat/heartbeat.go:243` (modify) | derive "is the human" via `store.IsHuman` instead of `== ""` |
| `internal/store/questions_test.go`, `agent_questions_test.go` (modify) | new behaviour |
| `internal/store/migrate_threads_test.go` (create) | data migration from a seeded pre-0009 database |
| `internal/store/migrate_realdb_test.go` (modify) | real-DB harness also reads threads |

---

## Interfaces produced (consumed by T2, subtask #731)

```go
const ParticipantHuman = "human"

// IsHuman reports whether a stored author/participant id denotes the human.
// Accepts both the legacy empty string and "human".
func IsHuman(id string) bool

type Question struct {
    ID         int64
    TaskID     int64  // 0 = not bound to a task
    RoleID     string // "" = not bound to a role
    AskedBy    string
    Body       string
    Context    string
    Status     string // open|resolved
    Resolution string // answered|dismissed
    AskedAt    int64
    ResolvedAt int64
}

type QuestionMessage struct {
    ID           int64
    QuestionID   int64
    Author       string   // "human" for the human
    Kind         string   // reply|answer
    Body         string
    AddressedTo  []string // nil/empty = everyone but the author
    CreatedAt    int64
}

func (s *Store) ListParticipants(questionID int64) ([]string, error)
func (s *Store) AddParticipants(questionID int64, ids ...string) error
func (s *Store) ListQuestionsForParticipant(participantID string, openOnly bool) ([]Question, error)
```

Unchanged signatures that must keep working: `AddQuestion`, `GetQuestion`,
`ListQuestions`, `ResolveQuestion`, `ReopenQuestion`, `AddQuestionMessage`,
`ListQuestionMessages`, `QuestionOrdinal`, `OpenQuestionCounts`,
`ListAllOpenQuestions`, and the whole `AgentQuestion*` family.

---

## Task 1: Migration `0009_threads.sql`

**Files:**
- Create: `internal/store/migrations/0009_threads.sql`
- Test: `internal/store/migrate_threads_test.go`

**Interfaces:**
- Consumes: schema of `0001_init.sql:94-113` and `0006_agent_questions.sql`.
- Produces: tables `questions`, `question_messages` (with `addressed_to`),
  `question_participants`.

The test builds a **pre-0009** database by applying migrations `0001`–`0008`
only, seeds both thread families, then calls `store.Open` so that only `0009`
runs, and asserts against the resulting tables with raw SQL (the Go DAO does not
exist yet at this point in the plan).

- [ ] **Step 1: Write the failing migration test**

`internal/store/migrate_threads_test.go`: helper `seedPre0009(t) string` opens a
raw `*sql.DB`, execs migrations `0001`…`0008` in order, inserts
`INSERT INTO schema_migrations (version) VALUES (1)…(8)`, seeds:
- task 1 with two questions (ids 1, 2), question 1 having messages from `''`
  (human) and `orch-1`;
- agent `cto` with two questions (ids 1, 2), question 2 having a message from
  `cto`.

Assertions after `Open`:
- `questions` holds 4 rows; the two task rows kept ids 1 and 2 with
  `role_id IS NULL`; the two role rows have `task_id IS NULL`,
  `role_id = 'cto'`, and ids greater than every task-thread id;
- every message still points at the thread it belonged to (compare bodies);
- per-thread ordinals (`COUNT(*) WHERE task_id/role_id = ? AND id <= ?`) are
  unchanged from before the migration;
- `question_participants` for the first task thread is exactly
  `{human, orch-1}`; for the second role thread exactly `{human, cto}`;
- `task_questions`, `question_messages`' old shape, `agent_questions` and
  `agent_question_messages` are gone (`sqlite_master` lookup returns no rows for
  the three dropped names).

- [ ] **Step 2: Run it and watch it fail**

`go test ./internal/store/ -run TestMigrate0009 -v` — expect failure: no such
table `questions`.

- [ ] **Step 3: Write the migration**

Order matters; each statement reads a table that has not been dropped yet.

```sql
-- 1. questions: task_questions rebuilt with task_id nullable + role_id.
--    Task-thread ids are carried over verbatim.
CREATE TABLE questions (...);
INSERT INTO questions (id, task_id, role_id, ...) SELECT id, task_id, NULL, ... FROM task_questions;

-- 2. role threads at a fixed offset = MAX(task_questions.id)
INSERT INTO questions (id, task_id, role_id, ...)
SELECT (SELECT COALESCE(MAX(id),0) FROM task_questions) + aq.id, NULL, aq.role_id, ...
FROM agent_questions aq;

-- 3./4. question_messages rebuilt with addressed_to, same offset trick
-- 5. drop the four old tables (their indexes go with them)
-- 6. rename question_messages_new -> question_messages
-- 7. question_participants with UNIQUE(question_id, participant_id)
-- 8. normalise authors: UPDATE question_messages SET author = 'human'
--    WHERE author IS NULL OR author = ''
-- 9. backfill participants: 'human', then asked_by, then every distinct author
-- 9. indexes: idx_questions_task, idx_questions_role,
--    idx_question_messages, idx_question_participants_participant
```

- [ ] **Step 4: Run the test — expect PASS**
- [ ] **Step 5: Commit** — `store: migration 0009 unifies question threads`

---

## Task 2: `Question.RoleID`, `QuestionMessage.AddressedTo`, `IsHuman`

**Files:**
- Modify: `internal/store/questions.go`, `internal/store/questions_reopen.go`
- Test: `internal/store/questions_test.go`

**Interfaces:**
- Consumes: Task 1's schema.
- Produces: `ParticipantHuman`, `IsHuman`, the extended structs.

- [ ] **Step 1: Failing tests** — `AddQuestionMessage` round-trips
  `AddressedTo` including the empty case; a human message written with
  `Author: ""` reads back as `"human"`; `AddQuestion` with `TaskID: 0` and
  `RoleID: "cto"` round-trips.
- [ ] **Step 2: Run, watch fail** (`AddressedTo` undefined).
- [ ] **Step 3: Implement** — point every query at `questions` /
  `question_messages`, add the two columns, CSV encode/decode helpers
  (`""` ⇄ `nil`), `nullIfZero(q.TaskID)` / `nullIfEmpty(q.RoleID)` on write,
  normalise `author` on read and write.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** — `store: questions carry role_id and addressed_to`

---

## Task 3: Participants API

**Files:**
- Modify: `internal/store/questions.go`
- Test: `internal/store/questions_test.go`

- [ ] **Step 1: Failing tests** — `ListParticipants` sorted deterministically;
  `AddParticipants` idempotent (adding twice is a no-op, not an error) and
  variadic; `ListQuestionsForParticipant("cto", true)` returns exactly the open
  threads where `cto` participates, ascending by id.
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement** — `INSERT OR IGNORE` in one transaction for
  idempotency under concurrency; `ORDER BY participant_id`.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** — `store: question participants`

---

## Task 4: Aggregates and ordinals under the unified table

**Files:**
- Modify: `internal/store/questions.go`, `internal/store/agent_questions.go`
- Test: `internal/store/questions_test.go`, `agent_questions_test.go`

The unified table means `OpenQuestionCounts` (keyed by task id) must exclude
role threads and `OpenAgentQuestionCounts` (keyed by role id) must exclude task
threads — otherwise each leaks the other's rows. Both `turn_user` predicates
must treat `''` and `'human'` alike.

- [ ] **Step 1: Failing tests** — with one open task thread and one open role
  thread present, `OpenQuestionCounts` has exactly one entry (the task) and
  `OpenAgentQuestionCounts` exactly one (the role); a thread whose last message
  author is `'human'` counts as *not* awaiting the user, same as `''`;
  `ListAllOpenQuestions` returns task threads only.
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement** — add `task_id IS NOT NULL` / `role_id IS NOT NULL`
  guards and replace `author != ''` with `author NOT IN ('', 'human')`.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** — `store: scope thread aggregates to their subject`

---

## Task 5: `agent_questions.go` as a facade

**Files:**
- Modify: `internal/store/agent_questions.go`
- Test: `internal/store/agent_questions_test.go`

Every exported `AgentQuestion*` function stays, implemented against `questions`
/ `question_messages` with `role_id` set. The existing tests in
`agent_questions_test.go` are the regression suite: they must pass unmodified
except where they assert the normalised author.

- [ ] **Step 1:** Run `go test ./internal/store/ -run AgentQuestion` and watch
  it fail against the dropped tables.
- [ ] **Step 2: Implement** the facade.
- [ ] **Step 3: Run — PASS.**
- [ ] **Step 4: Commit** — `store: agent questions become a facade over questions`

---

## Task 6: Role deletion cascade

**Files:**
- Modify: `internal/store/agents.go:107-135`
- Test: `internal/store/agents_test.go`

- [ ] **Step 1: Failing test** — create role `cto` with a thread, a message and
  participants; `DeleteAgent("cto")`; assert zero rows remain in `questions`,
  `question_messages` and `question_participants` for that role, and that an
  unrelated task thread is untouched.
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement** — delete participants, then messages, then
  questions, inside the existing transaction.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** — `store: role deletion clears its threads and participants`

---

## Task 7: Fix the out-of-store "author is the human" comparisons

**Files:**
- Modify: `internal/api/questions.go:68`, `internal/api/agent_questions.go:47`,
  `internal/heartbeat/heartbeat.go:243`

These three derive "the last entry came from the human" from `Author == ""`.
Once the store normalises to `"human"` they would silently invert `whose_turn`.
This is the one deliberate step outside `internal/store`, required by acceptance
criterion 8.

- [ ] **Step 1:** Run `go test ./internal/api/... ./internal/heartbeat/...` and
  record which tests fail (this is the failing-test step — the existing suites
  already cover the behaviour).
- [ ] **Step 2: Implement** — replace each comparison with `store.IsHuman(...)`.
- [ ] **Step 3: Run — PASS.**
- [ ] **Step 4: Commit** — `api,heartbeat: derive the human author via store.IsHuman`

---

## Task 8: Verification and PR

- [ ] `go build ./...`
- [ ] `go test ./...`
- [ ] `ROCKET_MIGTEST_DB=<copy of ~/.rocket/rocket.db> go test ./internal/store/ -run RealDB -v`
      — the migration applies to the real database and its threads still read.
- [ ] `gh pr create` referencing feature `reply-answer` and subtask #730.

---

## Self-review notes

- Spec coverage: spec §1 (data model) → Tasks 1–5; §2 `waiting_on` and §3 rights
  are T2's, not this plan's — this plan only supplies `ListParticipants` and
  `AddressedTo` that T2 computes them from.
- Open behavioural choice flagged to the orchestrator (message 7985):
  `ListAllOpenQuestions` keeps returning task threads only, preserving today's
  dashboard; widening it to role threads is an API-level decision for T2.
