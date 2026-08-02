# Persistent agents v4 (backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or
> superpowers:subagent-driven-development to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the managed persistent-agent layer (wake engine, roles, dossier,
GitHub subscriptions, memory) with a minimal model: an agent is a DB row plus a
fixed tmux session name, with a pull-based message inbox and a thin launcher.

**Architecture:** `agents` becomes `{id, description, project_id, dir, command,
enabled}`; `agent_inbox` becomes a plain message log with `unread|read`.
`rocket send <agent-id>` delivers through the normal message queue when the tmux
session named `<id>` is alive, otherwise it appends an inbox row. A small daemon
watcher (`internal/agentwatch`) adopts/retires the session row for each agent by
observing tmux, and injects a one-shot "N unread" notification when the session
appears. The agent drains its inbox itself via `rocket inbox [next|peek]`.
`rocket agent start|attach|stop` is a launcher with no lifecycle management.

**Tech Stack:** Go 1.x, SQLite (modernc.org/sqlite), cobra CLI, tmux runtime.

## Global Constraints

- Spec authority: task #639 doc "Спека: постоянные агенты (роли)" v4.
- `kind='agent'` stays a valid session kind; it now marks the externally created
  tmux session registered under the agent name.
- Q&A threads (`agent_questions`, `agent_question_messages`, their API and CLI)
  stay, except that delivery of a human entry uses the same live-or-inbox path
  as any message.
- Old `agent_inbox` rows are NOT migrated (feature unused in production);
  the migration comment must say so.
- Do not modify `web/` or `mobile/` (subtasks #705/#706).
- Acceptance gate: `go build ./... && go vet ./... && env -u ROCKET_SOCKET go test -count=1 ./...`.
- E2E must run with BOTH `ROCKET_HOME` and `ROCKET_SOCKET` overridden into the
  scratchpad — never bind the real socket.
- Migration file number: `0008_agents_v4.sql` (0007 is the last one in main; the
  brief's "0009" predates checking the tree).

---

## File Structure

Deleted:
- `internal/agentrun/` (all files), `internal/roles/` (all files)
- `internal/session/role.go`, `internal/session/role_test.go`
- `internal/ghpoller/roles.go`, `internal/ghpoller/roles_test.go`
- `internal/store/agent_items.go`, `internal/store/agent_github.go` (+ tests)
- `internal/prompts/templates/agent.md`, `docs/prompts/agent.md`

Created:
- `internal/store/migrations/0008_agents_v4.sql`
- `internal/agentwatch/watch.go` (+ `watch_test.go`) — tmux liveness → session
  row adoption + unread notification
- `internal/api/agent_delivery.go` (+ test) — the single live-or-inbox delivery
  helper used by messages, UI messages and Q&A threads
- `internal/cli/inbox.go` (+ test) — `rocket inbox [next|peek]`

Rewritten:
- `internal/store/agents.go`, `internal/store/agent_inbox.go` (+ tests)
- `internal/api/agents.go` (+ test), `internal/cli/agent.go` (+ test)
- `internal/session/agent.go` (new, replaces role.go) — `StartAgent`/`StopAgent`
- `docs/10-agents.md`; touched sections of `03-daemon-api.md`, `04-cli.md`,
  `05-state.md`, `06-messaging.md`, `09-github.md`

---

### Task 1: Schema v4

**Files:**
- Create: `internal/store/migrations/0008_agents_v4.sql`
- Modify: `internal/store/agents.go`, `internal/store/agent_inbox.go`
- Delete: `internal/store/agent_items.go`, `internal/store/agent_github.go`,
  `internal/store/agent_github_test.go`
- Test: `internal/store/agents_test.go`, `internal/store/agent_inbox_test.go`

**Interfaces:**
- Produces:
  - `store.Agent{ID, Description, ProjectID, Dir, Command string; Enabled bool; CreatedAt, UpdatedAt int64}`
  - `(*Store) AddAgent(Agent) error`, `GetAgent(id) (Agent, error)`,
    `ListAgents(projectID string) ([]Agent, error)`, `UpdateAgent(Agent) error`,
    `DeleteAgent(id string) error`
  - `store.InboxMessage{ID int64; AgentID, From, Body, Status string; CreatedAt, ReadAt int64}`
  - `store.InboxUnread = "unread"`, `store.InboxRead = "read"`
  - `(*Store) AddInboxMessage(InboxMessage) (int64, error)`
  - `(*Store) ListInboxMessages(agentID, status string, limit int) ([]InboxMessage, error)`
  - `(*Store) GetInboxMessage(id int64) (InboxMessage, error)`
  - `(*Store) NextUnreadInboxMessage(agentID string) (InboxMessage, bool, error)` —
    oldest unread, marks it read, returns ok=false when none
  - `(*Store) CountUnreadInbox(agentID string) (int, error)`
  - `(*Store) MaxUnreadInboxID(agentID string) (int64, error)` — 0 when none

- [ ] **Step 1: Write the migration**

`internal/store/migrations/0008_agents_v4.sql`:

```sql
-- Persistent agents v4 (task #639): the managed layer is gone. An agent is a
-- registry row plus a fixed tmux session name; rocket only registers it,
-- delivers messages and keeps an inbox.
--
-- Old agent_inbox rows are deliberately NOT migrated: the v2 event inbox
-- (issue_opened/cron/snooze_expired/...) has no meaning under v4 and the
-- feature never ran in production.
DROP TABLE IF EXISTS agent_gh_seen;
DROP TABLE IF EXISTS agent_gh_watermark;
DROP TABLE IF EXISTS agent_items;
DROP TABLE IF EXISTS agent_inbox;

CREATE TABLE agent_inbox (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id   TEXT NOT NULL REFERENCES agents(id),
  from_id    TEXT NOT NULL DEFAULT '',   -- sender session id; '' = human/UI
  body       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('unread','read')),
  created_at INTEGER NOT NULL,
  read_at    INTEGER
);

CREATE INDEX idx_agent_inbox_agent ON agent_inbox(agent_id, status, id);

-- agents is rebuilt: project_id becomes optional (grouping only) and the
-- managed columns (prompt_path, subscriptions, cron, agent) give way to the
-- launcher pair (dir, command).
CREATE TABLE agents_new (
  id          TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  project_id  TEXT NOT NULL DEFAULT '',
  dir         TEXT NOT NULL DEFAULT '',
  command     TEXT NOT NULL DEFAULT '',
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

INSERT INTO agents_new (id, description, project_id, dir, command, enabled, created_at, updated_at)
SELECT id, '', project_id, '', '', enabled, created_at, updated_at FROM agents;

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;

CREATE INDEX idx_agents_project ON agents(project_id);
```

- [ ] **Step 2: Rewrite the store DAOs and their tests, delete the dead ones**

Rewrite `store/agents.go` to the struct/methods above (drop
`AgentSubscription`, `PromptPath`, `Cron`, `Agent`; `DeleteAgent` no longer
touches `agent_items`). Rewrite `store/agent_inbox.go` to the message API above;
`NextUnreadInboxMessage` runs `SELECT ... WHERE agent_id=? AND status='unread'
ORDER BY id LIMIT 1` then `UPDATE agent_inbox SET status='read', read_at=?
WHERE id=? AND status='unread'` in one transaction. Delete `agent_items.go`,
`agent_github.go` and `agent_github_test.go`.

- [ ] **Step 3: Run `go test ./internal/store/... -count=1`** — expect green.

- [ ] **Step 4: Verify the migration against a copy of the real DB**

```bash
cp ~/.rocket/rocket.db /private/tmp/.../scratchpad/mig.db
go run ./cmd/rocket ...   # or a tiny Go program calling store.Open on the copy
sqlite3 /private/tmp/.../scratchpad/mig.db '.schema agents' '.schema agent_inbox'
```

- [ ] **Step 5: Commit** — `store: agents/inbox schema v4 (#639)`

---

### Task 2: Remove the managed layer

**Files:**
- Delete: `internal/agentrun/**`, `internal/roles/**`,
  `internal/session/role.go`, `internal/session/role_test.go`,
  `internal/ghpoller/roles.go`, `internal/ghpoller/roles_test.go`,
  `internal/prompts/templates/agent.md`, `docs/prompts/agent.md`
- Modify: `internal/daemon/daemon.go`, `internal/api/server.go`,
  `internal/ghpoller/poller.go`, `internal/prompts/prompts.go`,
  `internal/config/config.go`, `internal/store/tasks.go` (task_update events)

**Interfaces:**
- Produces: `config.Config.AgentNotifyInterval time.Duration` (yaml
  `agent_notify_interval`, default `5 * time.Minute`); `AgentIdleTimeout` and
  `AgentWakeDebounce` are removed. `api.Deps.Agents` is removed.

- [ ] **Step 1:** delete the packages/files above; drop `"agent"` from
  `prompts.Names()`; drop the `tickRoles` call from `ghpoller.Poller.tick`;
  drop the `task_update` inbox emission from the task-status path.
- [ ] **Step 2:** rewire `daemon.Run` — no `agentrun.New`, no `agentEngine.Run`,
  no `Agents:` in `api.Deps`.
- [ ] **Step 3:** `go build ./... && go vet ./...` — fix fallout only in Go code.
- [ ] **Step 4: Commit** — `agents: drop the managed layer (agentrun/roles/dossier/gh) (#639)`

---

### Task 3: Live-or-inbox delivery

**Files:**
- Create: `internal/api/agent_delivery.go`, `internal/api/agent_delivery_test.go`
- Modify: `internal/api/messages.go`, `internal/api/agent_questions.go`

**Interfaces:**
- Produces: `func deliverToAgent(d Deps, agentID, from, body string) (live bool, err error)`
  — if a session row `agentID` exists in state `spawning|running`, it stores a
  `store.Message` to it, publishes `message.queued` and calls `Queue.Wake`;
  otherwise it appends an unread `store.InboxMessage`.
- Consumes: Task 1 store API.

- [ ] **Step 1: Failing test** `TestDeliverToAgentQueuesWhenLive` /
  `...FallsBackToInbox` in `agent_delivery_test.go`, using the existing api test
  harness (see `internal/api/agents_test.go` for the pattern).
- [ ] **Step 2:** run it, expect a compile failure.
- [ ] **Step 3:** implement `deliverToAgent`; replace `postMessageToRole`'s body
  with it; in `agent_questions.go` replace `deliverHumanEntry`'s inbox
  enqueue+`NotifyRole` with `deliverToAgent(d, roleID, "", "[role <id> Q<n>
  <entry>] <text>")` and change `callerIsRoleInstance` to
  `caller != nil && caller.Kind == "agent" && caller.ID == roleID`.
- [ ] **Step 4:** `go test ./internal/api/... -count=1`.
- [ ] **Step 5: Commit** — `api: one live-or-inbox delivery path for agents (#639)`

---

### Task 4: Launcher (session manager + API + CLI)

**Files:**
- Create: `internal/session/agent.go`, `internal/session/agent_test.go`
- Modify: `internal/api/agents.go`, `internal/cli/agent.go`

**Interfaces:**
- Produces:
  - `(*Manager) StartAgent(ctx context.Context, a store.Agent) (store.Session, error)`
    — errors `validationErr("agent_no_dir", ...)` when `a.Dir == ""`,
    `validationErr("agent_live", ...)` when the tmux session already exists;
    creates tmux session named `a.ID` (cwd `a.Dir`, command `a.Command` or the
    login shell, env `ROCKET_SESSION_ID=<id>`, `ROCKET_SOCKET=<cfg.SocketPath()>`),
    then upserts a session row `{ID: a.ID, Kind: "agent", ProjectID: a.ProjectID,
    FeatureSlug: a.ID, Agent: cfg.DefaultAgent, TmuxName: a.ID,
    WorktreePath: a.Dir, State: "running"}`.
  - `(*Manager) StopAgent(ctx context.Context, id string) error` — destroys the
    tmux session and marks the session row `done`; the registration stays.
  - `(*Manager) AdoptAgentSession(a store.Agent) (store.Session, error)` and
    `(*Manager) RetireAgentSession(id string) error` — used by Task 5.

- [ ] **Step 1: Failing tests** for `StartAgent` (no dir → error; happy path
  creates the runtime session and the row) with the fake runtime already used by
  `internal/session` tests.
- [ ] **Step 2:** run, expect failure.
- [ ] **Step 3:** implement `internal/session/agent.go`.
- [ ] **Step 4:** add `POST /v1/agents/{id}/start` and `POST /v1/agents/{id}/stop`
  and `POST /v1/agents/{id}/messages {body}` (→ `deliverToAgent`); wire
  `rocket agent start|attach|stop`; `attach` prints/execs
  `rt.AttachCommand({Name: id})` like `rocket attach`.
- [ ] **Step 5:** `go test ./internal/session/... ./internal/api/... ./internal/cli/... -count=1`.
- [ ] **Step 6: Commit** — `agents: thin launcher (start/attach/stop) (#639)`

---

### Task 5: Watcher + unread notification

**Files:**
- Create: `internal/agentwatch/watch.go`, `internal/agentwatch/watch_test.go`
- Modify: `internal/daemon/daemon.go`

**Interfaces:**
- Produces: `agentwatch.New(st *store.Store, rt runtime.Runtime, cfg *config.Config, wake func(string)) *Watcher`
  with `Run(ctx)` (ticker `cfg.ActivityPollInterval`) and `Tick(ctx)`.
- Behaviour per tick, for every enabled agent:
  - tmux has `<id>` and no live session row → `AdoptAgentSession`, unless a
    session row `<id>` already exists with a different kind
    (orchestrator/worker) — then log once and skip (name-collision guard);
  - tmux lacks `<id>` and a live row exists → `RetireAgentSession` and forget the
    agent's notification bookkeeping;
  - session live and `CountUnreadInbox > 0` and
    `MaxUnreadInboxID > lastNotifiedID` and `now - lastNotifiedAt >=
    cfg.AgentNotifyInterval` → store a system message
    `"[rocket] You have N unread messages. Read them one by one with: rocket inbox next"`
    to `<id>`, `wake(id)`, and record `lastNotifiedID`/`lastNotifiedAt`.

- [ ] **Step 1: Failing tests:** adoption, retirement, notify-once, no re-notify
  without new unread, re-notify after a new unread + interval, notification after
  a session restart.
- [ ] **Step 2:** run, expect failure. **Step 3:** implement. **Step 4:** tests green.
- [ ] **Step 5:** wire `go w.Run(ctx)` into `daemon.Run`.
- [ ] **Step 6: Commit** — `agentwatch: session liveness + one-shot unread notice (#639)`

---

### Task 6: Pull CLI (`rocket inbox`)

**Files:**
- Create: `internal/cli/inbox.go`, `internal/cli/inbox_test.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `GET /v1/agents/{id}/inbox?status=unread`,
  `POST /v1/agents/{id}/inbox/next`, `GET /v1/agents/{id}/inbox/{msgID}`
  (added in this task to `internal/api/agents.go`).
- Produces: `rocket inbox [--agent <id>]`, `rocket inbox next`,
  `rocket inbox peek <msg-id>`. The agent id resolves from `ROCKET_SESSION_ID`,
  else from `tmux display-message -p '#S'` when `$TMUX` is set, else the
  `--agent` flag, else a usage error naming all three.

- [ ] **Step 1: Failing tests** for id resolution and for the three commands
  against the fake API server used by `internal/cli` tests.
- [ ] **Step 2:** run, expect failure. **Step 3:** implement (API handlers +
  CLI). **Step 4:** tests green.
- [ ] **Step 5: Commit** — `cli: pull-based rocket inbox (#639)`

---

### Task 7: Registry API/CLI surface v4

**Files:**
- Modify: `internal/api/agents.go`, `internal/api/agents_test.go`,
  `internal/cli/agent.go`, `internal/cli/agent_test.go`

**Interfaces:**
- Produces the final JSON contract (report it to the orchestrator):

```json
{ "id": "sre", "description": "…", "project": "rocket", "dir": "/path",
  "command": "claude --dangerously-skip-permissions", "enabled": true,
  "session_alive": true, "unread": 3, "open_questions": 1, "awaiting_user": 0,
  "created_at": 0, "updated_at": 0 }
```

```json
{ "id": 12, "from": "task-639-orch", "body": "…", "status": "unread",
  "created_at": 0, "read_at": 0 }
```

- Removed endpoints: `POST /v1/agents/{id}/wake`, `.../done`, `.../items`
  (GET/PUT), `.../memory` (GET/PUT).
- `POST /v1/agents` accepts `{id, description, project?, dir?, command?}`;
  `PATCH` accepts the same fields plus `enabled`. `project` is validated only
  when non-empty.
- CLI: `rocket agent add <id> [--description] [--project] [--dir] [--command]`,
  `ls`, `show`, `rm`, `enable`, `disable`, `start`, `attach`, `stop`,
  plus the untouched `ask|questions|reply|answer`. Gone: `wake`, `state`, `done`.

- [ ] **Step 1:** update the API tests to the new shapes (failing).
- [ ] **Step 2:** run, expect failure. **Step 3:** implement. **Step 4:** green.
- [ ] **Step 5: Commit** — `api,cli: agent registry surface v4 (#639)`

---

### Task 8: Docs

**Files:**
- Rewrite: `docs/10-agents.md`
- Modify: `docs/03-daemon-api.md`, `docs/04-cli.md`, `docs/05-state.md`,
  `docs/06-messaging.md`, `docs/09-github.md`

- [ ] **Step 1:** rewrite `docs/10-agents.md` to v4: model, registration,
  session, launcher, inbox + notification, Q&A threads, and a copy-paste
  reference snippet for the agent's own `CLAUDE.md` covering
  `rocket inbox`/`inbox next`/`inbox peek`/`rocket send`/`rocket task`.
- [ ] **Step 2:** grep the other docs for `agentrun|роль|prompt_path|подписк|досье|memory`
  and rewrite the touched sections.
- [ ] **Step 3: Commit** — `docs: persistent agents v4 (#639)`

---

### Task 9: Verification + E2E

- [ ] **Step 1:** `go build ./... && go vet ./... && env -u ROCKET_SOCKET go test -count=1 ./...`
- [ ] **Step 2:** E2E in the scratchpad with `ROCKET_HOME` **and** `ROCKET_SOCKET`
  overridden: register an agent → `rocket send <id>` with no session (lands in
  inbox) → `rocket agent start` → unread notification arrives in the pane →
  `rocket inbox next` drains one by one → `rocket send` to the live session
  injects directly → a Q&A human reply reaches the live session.
- [ ] **Step 3:** open the PR, report the JSON contracts to the orchestrator.
