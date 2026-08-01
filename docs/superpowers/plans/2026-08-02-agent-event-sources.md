# Agent role event sources (issues, comments, task_update) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Feed persistent agent roles (task #639, subtask #643) with durable GitHub and
task events: `issue_opened` / `issue_comment` from a per-role subscription poller, and
`task_update` whenever a task referenced from a role dossier changes status.

**Architecture:** `internal/ghpoller` gains a role pass inside the existing `Tick` (same
2m interval, same `*github.Client` with ETag/backoff). Per (role, repo) durable state
lives in two new SQLite tables: a first-seen watermark and a seen-id set, so a daemon
restart never re-enqueues. The tasks side hooks `store.UpdateTaskStatus`, the single
choke point every status transition (API, ghpoller reactions, session cancel) already
goes through.

**Tech Stack:** Go 1.2x single module, SQLite (modernc, embedded migrations in
`internal/store/migrations`), stdlib `net/http/httptest` for GitHub fakes.

## Global Constraints

- Migration number reserved for this task: `0007_agent_github.sql` (0006 = qa-threads,
  0008 = runtime). Keep wiring additive; expect a rebase before merge.
- Daemon never writes to GitHub — read-only endpoints only.
- English identifiers, comments and commit messages; Russian only in user-facing docs.
- ids `[a-z0-9-]`; follow existing package patterns (per-entity store file + test,
  ghpoller file + test).
- `go test ./...` must pass (`env -u ROCKET_SOCKET` for the env-sensitive
  `internal/cli` test).

## Design decisions

1. **Durable dedup** = `agent_gh_seen(role_id, repo, kind, external_id)` PRIMARY KEY.
   `MarkAgentGHSeen` is `INSERT ... ON CONFLICT DO NOTHING`; it returns `true` only for
   the first insert, so "first time seen" and "already enqueued" are one atomic decision
   that survives restarts.
2. **Seed on first poll**: the first tick for a (role, repo) records a watermark
   (`agent_gh_watermark.since` = now) and marks every currently open issue as seen
   *without* enqueueing. Rationale: subscribing a role to a busy repo must not dump the
   whole open-issue backlog into its inbox. After the seed, only issues/comments created
   after the watermark are candidates.
3. **Self-authored events**: the daemon token and the human owner are the same GitHub
   account, so authorship cannot distinguish the role's own `gh` comment from the
   owner's. An author-login heuristic would silently drop the spec's reopen scenario
   ("doesn't work, please fix" from the owner on a dossier issue). Decision from the
   orchestrator: roles sign every GitHub write with an invisible HTML trailer
   `<!-- rocket-agent:<role-id> -->` (mandated by the `agent.md` system prompt, owned by
   the runtime task). The poller skips any issue/comment whose body carries a marker of
   **any** role — self-loops and role-to-role loops both become impossible, humans are
   never misclassified, and no `GetUser` lookup is needed.
4. **task_update in the store layer**: `store.UpdateTaskStatus` reads the old status and
   title, performs the update, then enqueues one `task_update` event per distinct role
   whose `agent_items.task_id` references the task. Putting it here covers manual moves
   (`internal/api/tasks.go`), PR-driven moves (`internal/ghpoller/reactions.go`) and
   session cancellation with one implementation. No-op transitions (old == new) do not
   enqueue.
5. **Payload truncation**: issue/comment bodies are truncated to 4096 bytes on a rune
   boundary with a trailing `…` marker, so the inbox stays small.

## File structure

- Create `internal/store/migrations/0007_agent_github.sql` — watermark + seen tables.
- Create `internal/store/agent_github.go` — watermark/seen DAO (+ prune).
- Create `internal/store/agent_github_test.go`.
- Modify `internal/store/tasks.go` — `UpdateTaskStatus` enqueues `task_update`.
- Create `internal/store/tasks_agent_test.go` — task_update enqueue tests.
- Modify `internal/github/client.go` — `Issue` gains `User`/`CreatedAt`;
  add `ListIssuesSince`, `IssueComment`, `ListIssueCommentsSince`.
- Modify `internal/github/client_test.go` — tests for the new calls.
- Create `internal/ghpoller/roles.go` — role subscription pass.
- Create `internal/ghpoller/roles_test.go` — httptest-driven poller tests.
- Modify `internal/ghpoller/poller.go` — call the role pass from `Tick`.
- Modify `docs/09-github.md`, `docs/10-agents.md` (if present) — document the sources.

---

### Task 1: durable dedup state (migration + store DAO)

**Files:**
- Create: `internal/store/migrations/0007_agent_github.sql`
- Create: `internal/store/agent_github.go`
- Test: `internal/store/agent_github_test.go`

**Interfaces produced:**
- `store.GHSeenIssue = "issue_opened"`, `store.GHSeenComment = "issue_comment"`
- `(s *Store) AgentGHWatermark(roleID, repo string) (int64, error)` — 0 when unseeded
- `(s *Store) SetAgentGHWatermark(roleID, repo string, since int64) error`
- `(s *Store) MarkAgentGHSeen(roleID, repo, kind string, externalID int64) (bool, error)`
- `(s *Store) PruneAgentGHSeen(before int64) error`

- [ ] **Step 1: Write the failing test** in `agent_github_test.go`: open a store, add a
      project + agent, assert `AgentGHWatermark` returns 0, set it to 1000, read back
      1000; `MarkAgentGHSeen(role,"o/r","issue_opened",7)` returns true then false;
      a different kind/repo/role with the same id returns true; `PruneAgentGHSeen(now+1)`
      drops rows so marking 7 again returns true.
- [ ] **Step 2: Run** `go test ./internal/store -run AgentGH` → FAIL (undefined).
- [ ] **Step 3: Implement** the migration:

```sql
CREATE TABLE agent_gh_watermark (
  role_id    TEXT NOT NULL REFERENCES agents(id),
  repo       TEXT NOT NULL,
  since      INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (role_id, repo)
);

CREATE TABLE agent_gh_seen (
  role_id     TEXT NOT NULL REFERENCES agents(id),
  repo        TEXT NOT NULL,
  kind        TEXT NOT NULL,
  external_id INTEGER NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (role_id, repo, kind, external_id)
);

CREATE INDEX idx_agent_gh_seen_created ON agent_gh_seen(created_at);
```

      and the DAO in `agent_github.go` (INSERT ... ON CONFLICT DO NOTHING +
      `RowsAffected() > 0`).
- [ ] **Step 4: Run** `go test ./internal/store` → PASS.
- [ ] **Step 5: Commit** `feat(store): durable dedup state for role GitHub polling`.

---

### Task 2: GitHub client — issues since + issue comments

**Files:**
- Modify: `internal/github/client.go`
- Test: `internal/github/client_test.go`

**Interfaces produced:**
- `github.Issue` gains `User struct{ Login string } \`json:"user"\`` and
  `CreatedAt string \`json:"created_at"\``
- `(c *Client) ListIssuesSince(ctx, owner, repo, state string, since time.Time) ([]Issue, error)`
  — `since` zero means no `since` parameter; PRs filtered out, pagination followed.
- `type IssueComment struct { ID int64; IssueNumber int; Body, HTMLURL, CreatedAt string; User struct{Login string} }`
  where `IssueNumber` is derived from the API `issue_url` tail.
- `(c *Client) ListIssueCommentsSince(ctx, owner, repo string, since time.Time) ([]IssueComment, error)`
  — `GET /repos/{o}/{r}/issues/comments?since=...&per_page=100&sort=created&direction=asc`.

- [ ] **Step 1: Write the failing tests**: httptest server asserting the query string
      carries `since=2026-08-02T00:00:00Z`, returning two issues (one with
      `pull_request`) and two comments with `issue_url` ending `/issues/12`; assert PR
      filtered, `User.Login`, `CreatedAt`, and `IssueNumber == 12`.
- [ ] **Step 2: Run** `go test ./internal/github -run Since` → FAIL.
- [ ] **Step 3: Implement**: refactor `ListIssues` to delegate to `ListIssuesSince` with
      a zero time; add the comment type, `issueNumberFromURL` helper, and the paginated
      list call.
- [ ] **Step 4: Run** `go test ./internal/github` → PASS.
- [ ] **Step 5: Commit** `feat(github): list issues since and repo issue comments`.

---

### Task 3: role subscription poller

**Files:**
- Create: `internal/ghpoller/roles.go`
- Modify: `internal/ghpoller/poller.go` (Tick calls `p.tickRoles`)
- Test: `internal/ghpoller/roles_test.go`

**Interfaces produced:**
- `(p *Poller) tickRoles(ctx context.Context, client *github.Client) error`
- payload JSON: `{repo, number, title, author, body, html_url, labels[]}` for
  `issue_opened`; the same plus `comment_id` and the comment body/author for
  `issue_comment`.

Behaviour, in order, per enabled role × subscription:
1. Resolve watermark. Unseeded → mark all currently open issues seen, set watermark to
   `time.Now()`, enqueue nothing, continue with the next subscription.
2. `ListIssuesSince(state=open, since=watermark)`: skip PRs; skip issues created at or
   before the watermark; apply the label filter (any-match) and the `mention_only`
   filter (`@<role>` in title or body); skip bodies carrying a
   `<!-- rocket-agent:... -->` marker; `MarkAgentGHSeen(issue_opened, number)` and
   enqueue on first sight.
3. `ListIssueCommentsSince(since=watermark)`: keep a comment when its issue is in the
   dossier (`agent_items` kind=issue ref `owner/repo#N`) **or** its body mentions
   `@<role>`; skip marker-carrying bodies; `MarkAgentGHSeen(issue_comment, id)` and
   enqueue on first sight.
4. Advance the watermark to the newest processed `created_at` (never backwards) so the
   next tick asks GitHub for less.
5. `github.ErrBackoff` aborts the whole role pass; `ErrForbidden` warns once per
   (repo, endpoint) via `warnPermissionOnce` and skips that subscription.

- [ ] **Step 1: Write failing tests**: fake GitHub via httptest; seed test asserts zero
      inbox events on the first tick; second tick with a new issue enqueues exactly one
      `issue_opened` with the expected payload; a third tick with the same data enqueues
      nothing; a fresh `Poller` over the same store also enqueues nothing (restart
      dedup); label filter and `mention_only` negative cases; comment on a dossier issue
      enqueues `issue_comment`; comment on an unrelated issue without a mention does not;
      a comment carrying `<!-- rocket-agent:sre -->` is skipped even when it mentions
      `@sre`, and one carrying another role's marker is skipped too; a plain owner
      comment on a dossier issue IS enqueued (the spec's reopen scenario); disabled role
      gets nothing.
- [ ] **Step 2: Run** `go test ./internal/ghpoller -run Role` → FAIL.
- [ ] **Step 3: Implement** `roles.go` and the one-line `Tick` call.
- [ ] **Step 4: Run** `go test ./internal/ghpoller` → PASS.
- [ ] **Step 5: Commit** `feat(ghpoller): poll role subscriptions into the agent inbox`.

---

### Task 4: task_update events

**Files:**
- Modify: `internal/store/tasks.go`
- Test: `internal/store/tasks_agent_test.go`

**Interfaces produced:**
- `store.UpdateTaskStatus` side effect: one `task_update` inbox event per distinct role
  whose dossier references the task; payload
  `{task_id, title, from, to}`.

- [ ] **Step 1: Write failing tests**: role + dossier item with `task_id`; move the task
      `backlog → in_progress` and assert exactly one queued `task_update` with the right
      payload; a no-op transition to the same status enqueues nothing; a task nobody
      references enqueues nothing; two roles referencing the same task get one event each.
- [ ] **Step 2: Run** `go test ./internal/store -run TaskUpdateInbox` → FAIL.
- [ ] **Step 3: Implement**: read task before update, then
      `SELECT DISTINCT role_id FROM agent_items WHERE task_id = ?` and enqueue.
      Enqueue failures are returned as errors only after the status write succeeded —
      log-and-continue is not used, the caller must see a broken inbox.
- [ ] **Step 4: Run** `go test ./internal/store ./internal/api ./internal/ghpoller` → PASS.
- [ ] **Step 5: Commit** `feat(tasks): emit task_update inbox events for dossier tasks`.

---

### Task 5: docs + full verification

**Files:**
- Modify: `docs/09-github.md` (role subscription polling section)
- Modify: `docs/10-agents.md` if it exists (event sources)

- [ ] **Step 1** Document: what is polled, the filters, the seed-on-first-poll rule, the
      dedup guarantee, the self-author rule, the `task_update` trigger.
- [ ] **Step 2** Run `go build ./... && go vet ./... && env -u ROCKET_SOCKET go test ./...`.
- [ ] **Step 3** Commit and open the PR against `main` referencing feature task-639.
