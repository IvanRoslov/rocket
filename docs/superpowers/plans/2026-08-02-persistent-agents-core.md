# Persistent Agents — `core` Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans /
> superpowers:test-driven-development to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist agent roles in the rocket daemon — registry, inbox and dossier
tables, REST `/v1/agents`, `rocket agent` CLI, and the per-role home directory —
with no runtime/wake engine yet (task #642).

**Architecture:** Follow the existing layering exactly: one migration file per
schema change (`internal/store/migrations`), one DAO file per entity in
`internal/store`, one handler file per resource in `internal/api` registered from
`NewHandler`, one cobra command tree in `internal/cli` talking to the daemon over
the unix socket. Role home directories (`<home>/agents/<id>/role.md`,
`memory/MEMORY.md`) are created by the daemon in a small `internal/roles`
package (name avoids the existing `internal/agent`, which means the *underlying*
coding agent — claude-code/codex).

**Tech Stack:** Go 1.2x single module, SQLite via `modernc.org/sqlite`, cobra CLI,
`net/http` ServeMux with method+path patterns, stdlib `testing`.

## Global Constraints

- Role id pattern: `^[a-z0-9-]+$` (same `idPattern` as projects/repos).
- Spec: task #639 doc «Спека: постоянные агенты (роли)» v2; decomposition plan
  task 1 (`core`).
- No GitHub writes, no session spawning in this layer. `rocket agent wake` only
  enqueues a `message` inbox event.
- Inbox event kinds: `message | issue_opened | issue_comment | task_update |
  snooze_expired | cron | question | terminal_opened`.
- Inbox statuses: `queued | delivered | done`.
- Dossier item kinds: `issue | task | ping`; states are free-form strings
  (`new|triaged|taken|deferred|waiting_team|in_work|resolved|closed` by
  convention, not enforced — it is the agent's notebook).
- English identifiers and commit messages; Russian CLI help strings (matches the
  rest of `internal/cli`).
- Branch `feature/task-639/core`, one PR.

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/store/migrations/0005_agents.sql` | new tables + `sessions.kind` gains `agent` |
| `internal/store/store.go` | migrations run on a dedicated conn with `foreign_keys=OFF` |
| `internal/store/agents.go` | `Agent` DAO (CRUD, subscriptions JSON) |
| `internal/store/agent_inbox.go` | inbox event DAO |
| `internal/store/agent_items.go` | dossier DAO (upsert by (role, kind, ref)) |
| `internal/roles/roles.go` | role home dir scaffold + prompt/memory read/write |
| `internal/api/agents.go` | `/v1/agents` CRUD + `/inbox`, `/items`, `/wake` |
| `internal/cli/agent.go` | `rocket agent add|ls|show|rm|enable|disable|wake|state` |
| `docs/03-daemon-api.md`, `docs/04-cli.md`, `docs/10-agents.md` | documentation |

---

### Task 1: Migration + `sessions.kind = 'agent'`

**Files:**
- Create: `internal/store/migrations/0005_agents.sql`
- Modify: `internal/store/store.go` (migrate on a single conn, FK off)
- Test: `internal/store/agents_test.go` (new), `internal/store/store_test.go`

**Interfaces:**
- Produces: tables `agents`, `agent_inbox`, `agent_items`; `sessions.kind`
  accepts `agent`.

- [ ] **Step 1: Write failing test** `TestSessionKindAgentAllowed` — open a store
  in `t.TempDir()`, `AddSession(store.Session{ID:"sre-run-1", Kind:"agent", ...})`,
  expect no error. Plus `TestMigrationPreservesSessions`: insert a session and a
  task referencing it into a store opened on a DB created from migration 4 only —
  simpler equivalent: open store, add project/session/task, close, re-open (a
  no-op re-migrate) and check rows still readable.
- [ ] **Step 2:** `go test ./internal/store -run Agent` → FAIL (CHECK constraint).
- [ ] **Step 3:** write `0005_agents.sql`: rebuild `sessions` (`sessions_new` with
  `kind IN ('orchestrator','worker','agent')`, self-FK written as
  `REFERENCES sessions(id)`), copy rows, `DROP TABLE sessions`,
  `ALTER TABLE sessions_new RENAME TO sessions`, recreate
  `idx_sessions_state`/`idx_sessions_feature`; then `CREATE TABLE agents`,
  `agent_inbox`, `agent_items` + indexes. Change `migrate()` to grab a dedicated
  `*sql.Conn`, `PRAGMA foreign_keys=OFF` before the loop and `ON` after (the
  pragma is a no-op inside a transaction, and `DROP TABLE sessions` would
  otherwise trip `tasks.session_id`).
- [ ] **Step 4:** tests pass, `go test ./internal/store`.
- [ ] **Step 5:** commit `feat(store): agents/agent_inbox/agent_items schema, sessions.kind agent`.

Schema:

```sql
CREATE TABLE agents (
  id            TEXT PRIMARY KEY,
  project_id    TEXT NOT NULL REFERENCES projects(id),
  prompt_path   TEXT NOT NULL,
  subscriptions TEXT NOT NULL DEFAULT '[]',  -- JSON [{repo,labels[],mention_only}]
  cron          TEXT NOT NULL DEFAULT '',
  agent         TEXT NOT NULL DEFAULT '',
  enabled       INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE TABLE agent_inbox (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id    TEXT NOT NULL REFERENCES agents(id),
  kind       TEXT NOT NULL,
  payload    TEXT NOT NULL DEFAULT '{}',
  status     TEXT NOT NULL DEFAULT 'queued',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE agent_items (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id      TEXT NOT NULL REFERENCES agents(id),
  kind         TEXT NOT NULL,
  external_ref TEXT NOT NULL,
  state        TEXT NOT NULL DEFAULT 'new',
  note         TEXT NOT NULL DEFAULT '',
  task_id      INTEGER,
  snooze_until INTEGER,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE (role_id, kind, external_ref)
);
CREATE INDEX idx_agent_inbox_role ON agent_inbox(role_id, status, id);
CREATE INDEX idx_agent_items_role ON agent_items(role_id, state);
```

---

### Task 2: `store.Agent` DAO

**Files:** Create `internal/store/agents.go`; Test `internal/store/agents_test.go`.

**Interfaces / Produces:**

```go
type AgentSubscription struct {
    Repo        string   `json:"repo"`
    Labels      []string `json:"labels,omitempty"`
    MentionOnly bool     `json:"mention_only,omitempty"`
}
type Agent struct {
    ID, ProjectID, PromptPath, Cron, Agent string
    Subscriptions []AgentSubscription
    Enabled bool
    CreatedAt, UpdatedAt int64
}
func (s *Store) AddAgent(a Agent) error            // ErrExists on dup id
func (s *Store) GetAgent(id string) (Agent, error) // ErrNotFound
func (s *Store) ListAgents(projectID string) ([]Agent, error) // "" = all, ordered by id
func (s *Store) UpdateAgent(a Agent) error         // ErrNotFound; bumps updated_at
func (s *Store) DeleteAgent(id string) error       // ErrNotFound; cascades inbox+items
```

- [ ] Step 1: tests — add/get roundtrip incl. subscriptions JSON; duplicate → `ErrExists`;
  get missing → `ErrNotFound`; list filtered by project; update changes fields and
  `updated_at`; delete removes agent and its inbox/items rows.
- [ ] Step 2: run → FAIL. Step 3: implement (mirror `projects.go`).
- [ ] Step 4: `go test ./internal/store`. Step 5: commit `feat(store): agent registry DAO`.

---

### Task 3: inbox + dossier DAOs

**Files:** Create `internal/store/agent_inbox.go`, `internal/store/agent_items.go`;
tests in `internal/store/agent_inbox_test.go`, `internal/store/agent_items_test.go`.

**Produces:**

```go
type AgentInboxEvent struct {
    ID int64; RoleID, Kind, Payload, Status string; CreatedAt, UpdatedAt int64
}
func (s *Store) EnqueueInboxEvent(e AgentInboxEvent) (int64, error) // default status queued, payload {}
func (s *Store) ListInboxEvents(roleID, status string, limit int) ([]AgentInboxEvent, error) // newest-last, "" status = all
func (s *Store) QueuedInboxEvents(roleID string) ([]AgentInboxEvent, error)
func (s *Store) MarkInboxDelivered(ids []int64) error
func (s *Store) MarkInboxDone(ids []int64) error

type AgentItem struct {
    ID int64; RoleID, Kind, ExternalRef, State, Note string
    TaskID int64; SnoozeUntil int64; CreatedAt, UpdatedAt int64
}
func (s *Store) UpsertAgentItem(it AgentItem) (AgentItem, error) // by (role,kind,ref)
func (s *Store) ListAgentItems(roleID, state string) ([]AgentItem, error)
func (s *Store) GetAgentItem(roleID, kind, ref string) (AgentItem, error) // ErrNotFound
```

- [ ] Step 1: tests — enqueue defaults; queued-only listing; mark delivered/done
  transitions and `updated_at` bump; unknown role → error (FK); upsert insert then
  update keeps id and created_at, updates state/note/task/snooze; list by state.
- [ ] Steps 2–4: red → implement → green.
- [ ] Step 5: commit `feat(store): agent inbox and dossier DAOs`.

---

### Task 4: role home directory (`internal/roles`)

**Files:** Create `internal/roles/roles.go`, `internal/roles/roles_test.go`.

**Produces:**

```go
func Dir(home, id string) string        // <home>/agents/<id>
func PromptPath(home, id string) string // <home>/agents/<id>/role.md
func MemoryDir(home, id string) string  // <home>/agents/<id>/memory
// Ensure creates the dir tree (0700), writes role.md with prompt when the file
// does not exist yet (or when overwrite is true) and seeds memory/MEMORY.md
// with a header comment. Returns the prompt path.
func Ensure(home, id, prompt string, overwrite bool) (string, error)
func ReadPrompt(path string) (string, error)
```

- [ ] Step 1: tests — Ensure creates `role.md` + `memory/MEMORY.md`; second call
  without overwrite keeps existing prompt; with overwrite replaces it; dirs 0700.
- [ ] Steps 2–4: red → implement → green.
- [ ] Step 5: commit `feat(roles): per-role home directory scaffold`.

---

### Task 5: REST `/v1/agents`

**Files:** Create `internal/api/agents.go`, `internal/api/agents_test.go`;
Modify `internal/api/server.go` (register `registerAgentRoutes`).

Routes and JSON:

- `GET /v1/agents[?project=<id>]` → `[{id, project, prompt_path, subscriptions,
  cron, agent, enabled, inbox_queued, items, created_at, updated_at}]`
- `POST /v1/agents` `{id, project, prompt, prompt_path?, subscriptions?, cron?,
  agent?}` → 201 role JSON. Validates id pattern (`invalid_id`), project exists
  (`project_not_found`), duplicate (`agent_exists`). Writes the role home dir
  (`prompt` body → `role.md`), defaults `agent` to `Cfg.DefaultAgent`.
- `GET /v1/agents/{id}` → role JSON + `prompt` body.
- `PATCH /v1/agents/{id}` — `prompt`, `subscriptions`, `cron`, `agent`, `enabled`.
- `DELETE /v1/agents/{id}` → `{"status":"deleted"}` (files kept on disk).
- `GET /v1/agents/{id}/inbox[?status=queued]` → events.
- `POST /v1/agents/{id}/wake` `{text?, kind?}` → 202 `{event_id, kind}`; kind
  defaults to `message`, only kinds from the allowed set are accepted.
- `GET /v1/agents/{id}/items[?state=deferred]` → dossier.
- `POST /v1/agents/{id}/enable`, `POST /v1/agents/{id}/disable` → role JSON.
- `PUT /v1/agents/{id}/items` `{kind, ref, state, note?, task_id?, snooze_until?}`
  → upserted item. Session-authenticated like the task log: a caller with
  `X-Rocket-Session` must be a `kind=agent` session whose role (session id prefix
  `<role>-run-`) equals `{id}`; a human caller (no header) is always allowed.

- [ ] Step 1: table-driven handler tests using `sessionsTestDeps(t)` (real store,
  temp home): create → 201 and role dir exists; duplicate → 409; bad id → 400;
  unknown project → 400; list/get/patch/delete; wake enqueues and shows up in
  `/inbox`; unknown kind → 400; items PUT then GET with `state` filter.
- [ ] Steps 2–4: red → implement → green (`go test ./internal/api`).
- [ ] Step 5: commit `feat(api): /v1/agents CRUD, inbox and dossier endpoints`.

---

### Task 6: CLI `rocket agent`

**Files:** Create `internal/cli/agent.go`, `internal/cli/agent_test.go`;
Modify `internal/cli/root.go` (`root.AddCommand(newAgentCmd())`).

Commands (all `--json`-aware, tabwriter output otherwise):

```
rocket agent add <id> --project <p> --prompt-file role.md
                      [--watch owner/repo[,label=bug][,mention-only]]... [--cron "..."] [--agent claude-code]
rocket agent ls [--project <p>] | show <id> | rm <id> | enable <id> | disable <id>
rocket agent wake <id> ["text"]
rocket agent state set <kind>:<ref> <state> [--note "..."] [--until 2026-08-15] [--task 45]
rocket agent state ls [--state deferred] [--agent <id>]
```

`state` subcommands resolve the role from `--agent` or, when omitted, from
`ROCKET_SESSION_ID` (`<role>-run-<n>` → `<role>`), so a future role instance needs
no flags. `--until` accepts `YYYY-MM-DD` or RFC3339.

- [ ] Step 1: tests — usage errors for every command (wrong arg counts, missing
  `--project`/`--prompt-file`); `parseWatch("owner/repo,label=bug,mention-only")`
  → `{Repo:"owner/repo", Labels:["bug"], MentionOnly:true}`; `roleFromSessionID`
  (`"sre-run-3"` → `"sre"`, `"orch"` → `""`); `parseUntil` for both formats.
- [ ] Steps 2–4: red → implement → green (`go test ./internal/cli`).
- [ ] Step 5: commit `feat(cli): rocket agent commands`.

---

### Task 7: Documentation + verification

**Files:** Modify `docs/03-daemon-api.md`, `docs/04-cli.md`, `docs/10-agents.md`
(new "Роли (постоянные агенты)" section pointing at the spec), `docs/05-state.md`
(schema tables).

- [ ] Step 1: document the endpoints, CLI commands and tables exactly as built.
- [ ] Step 2: `make test` (or `go build ./... && go vet ./... && go test ./...`).
- [ ] Step 3: end-to-end smoke against a copy of the real DB: `ROCKET_HOME=$tmp`
  daemon run, `rocket agent add`, `ls`, `wake`, `state set/ls`, `rm`; plus open a
  copy of `~/.rocket/rocket.db` with the new binary to prove migration 5 applies
  to real data.
- [ ] Step 4: commit + `gh pr create` referencing feature `task-639`.
