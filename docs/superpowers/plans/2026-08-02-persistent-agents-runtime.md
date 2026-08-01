# Persistent Agents Runtime (wake engine + instance lifecycle) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development for each task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** make registered agent roles actually run: inbox events wake a single
ephemeral session `kind=agent` per role in a persistent worktree, briefed with
inbox + dossier + memory, ended by `rocket agent done` or an idle timeout.

**Architecture:** a new `internal/agentrun` package owns the engine (debounce,
single-instance lock, spawn-vs-inject decision, done/idle termination) and the
scheduler (snooze expiry + role cron). Session creation stays in
`internal/session` (`Manager.SpawnRole`) so all lifecycle mutations keep going
through the existing manager mutex. `internal/api` notifies the engine whenever
an inbox event is enqueued (`/v1/agents/{id}/wake`, `POST /v1/messages` to a
role id) and exposes `POST /v1/agents/{id}/done`.

**Tech Stack:** Go 1.25, SQLite (modernc), cobra CLI, tmux runtime. No new
dependencies (cron is a small hand-written parser).

## Global Constraints

- Task #642 of feature `task-639`; branch `feature/task-639/runtime`; one PR.
- Follow existing package patterns: per-entity store files, handler+test per
  API resource, cobra commands in `internal/cli`, table-driven Go tests.
- English identifiers, comments and commit messages; Russian only in CLI
  user-facing strings and docs, matching the surrounding code.
- ids `[a-z0-9-]`; role instance session id is `<role>-run-<n>` — the only link
  between a run and its role (no extra column).
- Role worktree is persistent and shared across runs:
  `<worktrees_dir>/<main-repo>/<role>-agent`, branch `agent/<role>`; never
  destroyed by the runtime, no PRs from it.
- Daemon never writes to GitHub.
- At most one live instance per role.

---

## File Structure

- `internal/store/agent_inbox.go` (modify) — `RolesWithQueuedInbox()`.
- `internal/store/agent_items.go` (modify) — `DueSnoozedItems(now)`,
  `ClearAgentItemSnooze(id)`.
- `internal/agentrun/cron.go` (create) — 5-field cron parser + `Next(after)`.
- `internal/agentrun/briefing.go` (create) — briefing text builder.
- `internal/agentrun/engine.go` (create) — engine: notify/debounce/process/done/
  idle timeout + Run loop (scheduler ticks).
- `internal/session/role.go` (create) — `Manager.SpawnRole`.
- `internal/prompts/templates/agent.md` (create) + `docs/prompts/agent.md`
  (create) + `internal/prompts/prompts.go` (modify `Names`).
- `internal/api/agents.go` (modify) — notify on wake, `POST .../done`.
- `internal/api/messages.go` (modify) — `to` may be a role id.
- `internal/api/server.go` (modify) — `Deps.Agents`.
- `internal/cli/agent.go` (modify) — `rocket agent done`.
- `internal/config/config.go` (modify) — `agent_idle_timeout`,
  `agent_wake_debounce`.
- `internal/daemon/daemon.go` (modify) — wire the engine.
- `docs/10-agents.md`, `docs/03-daemon-api.md`, `docs/04-cli.md`,
  `docs/05-state.md` (modify).

---

### Task 1: store queries the engine needs

**Files:**
- Modify: `internal/store/agent_inbox.go`, `internal/store/agent_items.go`
- Test: `internal/store/agent_inbox_test.go`, `internal/store/agent_items_test.go`

**Interfaces produced:**
- `func (s *Store) RolesWithQueuedInbox() ([]string, error)` — distinct role
  ids with at least one `queued` event, ordered by role id.
- `func (s *Store) DueSnoozedItems(now int64) ([]AgentItem, error)` — items
  with `snooze_until > 0 AND snooze_until <= now`, oldest first.
- `func (s *Store) ClearAgentItemSnooze(id int64) error` — sets
  `snooze_until = NULL`, stamps `updated_at`.

- [ ] **Step 1: failing tests**

```go
func TestRolesWithQueuedInbox(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	mustAddAgent(t, s, testAgent("sre"))
	mustAddAgent(t, s, testAgent("triage"))

	id, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "sre", Kind: "message"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "triage", Kind: "cron"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := s.MarkInboxDone([]int64{id}); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	got, err := s.RolesWithQueuedInbox()
	if err != nil {
		t.Fatalf("RolesWithQueuedInbox: %v", err)
	}
	if len(got) != 1 || got[0] != "triage" {
		t.Fatalf("want [triage], got %v", got)
	}
}

func TestDueSnoozedItemsAndClear(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	mustAddAgent(t, s, testAgent("sre"))

	due, err := s.UpsertAgentItem(AgentItem{RoleID: "sre", Kind: "issue", ExternalRef: "a/b#1", State: "deferred", SnoozeUntil: 100})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := s.UpsertAgentItem(AgentItem{RoleID: "sre", Kind: "issue", ExternalRef: "a/b#2", State: "deferred", SnoozeUntil: 5000}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	items, err := s.DueSnoozedItems(1000)
	if err != nil {
		t.Fatalf("DueSnoozedItems: %v", err)
	}
	if len(items) != 1 || items[0].ExternalRef != "a/b#1" {
		t.Fatalf("want a/b#1, got %+v", items)
	}

	if err := s.ClearAgentItemSnooze(due.ID); err != nil {
		t.Fatalf("ClearAgentItemSnooze: %v", err)
	}
	items, err = s.DueSnoozedItems(1000)
	if err != nil {
		t.Fatalf("DueSnoozedItems: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("want no due items after clear, got %+v", items)
	}
}
```

`mustAddAgent` is a new helper in `agents_test.go` wrapping `s.AddAgent`.

- [ ] **Step 2:** `go test ./internal/store/ -run 'QueuedInbox|Snoozed'` → FAIL (undefined).
- [ ] **Step 3:** implement the three methods with plain SQL, mirroring the
      existing helpers' error wrapping style.
- [ ] **Step 4:** rerun → PASS.
- [ ] **Step 5:** commit `feat(store): queries for the role wake engine`.

---

### Task 2: cron parser

**Files:**
- Create: `internal/agentrun/cron.go`, `internal/agentrun/cron_test.go`

**Interfaces produced:**
- `type Schedule struct{ ... }`
- `func ParseCron(expr string) (Schedule, error)` — standard 5 fields
  (minute hour dom month dow), supporting `*`, `*/n`, `a-b`, `a-b/n` and
  comma lists; `dow` 0-6 with 7 == 0.
- `func (s Schedule) Next(after time.Time) time.Time` — next matching minute
  strictly after `after` (local time, second precision truncated).

- [ ] **Step 1: failing test**

```go
func TestParseCronNext(t *testing.T) {
	tests := []struct {
		expr  string
		after string
		want  string
	}{
		{"0 * * * *", "2026-08-02T10:15:00Z", "2026-08-02T11:00:00Z"},
		{"*/15 * * * *", "2026-08-02T10:15:00Z", "2026-08-02T10:30:00Z"},
		{"30 9 * * 1", "2026-08-02T10:15:00Z", "2026-08-03T09:30:00Z"},
	}
	for _, tt := range tests {
		s, err := ParseCron(tt.expr)
		if err != nil {
			t.Fatalf("ParseCron(%q): %v", tt.expr, err)
		}
		after, _ := time.Parse(time.RFC3339, tt.after)
		want, _ := time.Parse(time.RFC3339, tt.want)
		if got := s.Next(after.UTC()); !got.Equal(want) {
			t.Errorf("%s: want %s, got %s", tt.expr, want, got)
		}
	}
}

func TestParseCronRejectsGarbage(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "61 * * * *", "a * * * *"} {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q): want error", expr)
		}
	}
}
```

- [ ] **Step 2:** run → FAIL. **Step 3:** implement (bitsets per field, minute
      scan bounded to 366 days). **Step 4:** run → PASS.
- [ ] **Step 5:** commit `feat(agentrun): minimal cron parser for role schedules`.

---

### Task 3: role system-prompt template

**Files:**
- Create: `internal/prompts/templates/agent.md`, `docs/prompts/agent.md`
- Modify: `internal/prompts/prompts.go` (`Names`)
- Test: `internal/prompts/prompts_test.go`

**Placeholders:** `{{role_id}}`, `{{project_name}}`, `{{main_repo}}`,
`{{main_repo_path}}`, `{{session_id}}`, `{{worktree_path}}`, `{{memory_dir}}`,
`{{role_prompt}}`.

- [ ] **Step 1: failing test**

```go
func TestRenderAgentTemplate(t *testing.T) {
	out, err := Render("", "agent", Vars{
		"role_id": "sre", "project_name": "Platform", "main_repo": "platform",
		"main_repo_path": "/repos/platform", "session_id": "sre-run-1",
		"worktree_path": "/wt/sre-agent", "memory_dir": "/home/agents/sre/memory",
		"role_prompt": "POLICY BODY",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"sre-run-1", "rocket agent done", "POLICY BODY",
		"/home/agents/sre/memory", "<!-- rocket-agent:sre -->"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
}

func TestNamesIncludesAgent(t *testing.T) {
	if !slices.Contains(Names(), "agent") {
		t.Fatalf("Names() = %v, want it to include agent", Names())
	}
}
```

The template body must state, at minimum:

- role identity and project; rocket is available; everything goes through
  tasks (`rocket task add` / `rocket task start`, own project only);
- dossier discipline (`rocket agent state set`) and memory discipline
  (`memory/MEMORY.md` index + fact files);
- boundaries from the spec's "Права и границы" (own project only, no direct
  worker spawning, GitHub writes only via `gh` from the instance itself);
- escalation to the human through role Q&A threads (`rocket agent reply` on
  the role side; the thread CLI lands with the sibling qa-threads PR);
- **GitHub write marker (contract with the github-tasks worker):** every
  GitHub write made with `gh` — issue comments, PR comments, issue bodies —
  must end with the invisible marker `<!-- rocket-agent:{{role_id}} -->`, so
  the poller can filter the role's own writes out of its inbox (the role
  shares the owner's token, so login-based filtering is impossible);
- finish a run with `rocket agent done`.

- [ ] **Step 2:** run → FAIL. **Step 3:** write the template per the list
      above, append `"agent"` to `Names`, mirror the text into
      `docs/prompts/agent.md` with a placeholder table like `worker.md`.
      **Step 4:** run → PASS (the test also asserts the marker string is
      present in the rendered prompt).
- [ ] **Step 5:** commit `feat(prompts): agent role system prompt template`.

---

### Task 4: `Manager.SpawnRole`

**Files:**
- Create: `internal/session/role.go`, `internal/session/role_test.go`

**Interfaces consumed:** `prompts.Render(home, "agent", …)` (Task 3).

**Interfaces produced:**
```go
// SpawnRole launches a fresh instance of role in its persistent worktree.
func (m *Manager) SpawnRole(ctx context.Context, role store.Agent, briefing string) (store.Session, error)
// RoleWorktreeName returns the persistent worktree/session-dir name of a role.
func RoleWorktreeName(roleID string) string // "<role>-agent"
// RoleBranch returns the persistent branch of a role. // "agent/<role>"
func RoleBranch(roleID string) string
```

Behaviour: resolve project + main repo; reject a disabled role
(`validationErr("agent_disabled", …)`); reserve id `<role>-run-<n>` (n = 1 +
highest existing `-run-<n>` for that role in the store, retrying on
`ErrExists`); persist session `kind=agent`, `FeatureSlug=role.ID`,
`Branch=agent/<role>`, state `spawning`; ensure the persistent worktree
(`ws.Restore` when the directory exists, `ws.Create` otherwise); render the
`agent` system prompt with the role prompt read via `roles.ReadPrompt`;
`SetupWorkspace`, env + token, `rt.Create`; mark `running`, publish
`agent.instance_spawned`.

- [ ] **Step 1: failing test** (uses the package's existing fake runtime and
      workspace doubles from `manager_test.go`):

```go
func TestSpawnRoleUsesPersistentWorktreeAndRunNumbering(t *testing.T) {
	m, st, fakeRT, fakeWS := newTestManager(t)
	seedProjectAndRepo(t, st)
	role := store.Agent{ID: "sre", ProjectID: "platform", PromptPath: writeRolePrompt(t, "POLICY"), Enabled: true, Agent: "fake"}
	if err := st.AddAgent(role); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	sess, err := m.SpawnRole(context.Background(), role, "BRIEFING")
	if err != nil {
		t.Fatalf("SpawnRole: %v", err)
	}
	if sess.ID != "sre-run-1" || sess.Kind != "agent" || sess.Branch != "agent/sre" {
		t.Fatalf("unexpected session %+v", sess)
	}
	if got := fakeWS.lastCreateSessionID; got != "sre-agent" {
		t.Fatalf("worktree name = %q, want sre-agent", got)
	}
	if !strings.Contains(fakeRT.lastCommand, "BRIEFING") {
		t.Fatalf("briefing not passed as first message: %q", fakeRT.lastCommand)
	}

	second, err := m.SpawnRole(context.Background(), role, "AGAIN")
	if err != nil {
		t.Fatalf("SpawnRole #2: %v", err)
	}
	if second.ID != "sre-run-2" {
		t.Fatalf("second run id = %q, want sre-run-2", second.ID)
	}
	if fakeWS.createCalls != 1 || fakeWS.restoreCalls != 1 {
		t.Fatalf("worktree reuse broken: create=%d restore=%d", fakeWS.createCalls, fakeWS.restoreCalls)
	}
}

func TestSpawnRoleRejectsDisabledRole(t *testing.T) { /* Enabled:false -> ValidationError code "agent_disabled" */ }
```

(The fake workspace gains `lastCreateSessionID`, `createCalls`, `restoreCalls`
counters and an on-disk directory it creates in `Create`, so the "exists →
Restore" branch is exercised.)

- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5:** commit `feat(session): spawn role instances in a persistent worktree`.

---

### Task 5: briefing builder

**Files:**
- Create: `internal/agentrun/briefing.go`, `internal/agentrun/briefing_test.go`

**Interfaces produced:**
```go
// BuildBriefing renders the first message of a role instance.
func BuildBriefing(st *store.Store, home string, role store.Agent, events []store.AgentInboxEvent) (string, error)
```

Sections, in order: `## Inbox` (each event: `#id kind` + human-readable payload
line — `text`/`from` for messages, raw JSON otherwise); `## Dossier` — items
referenced by the events (payload `ref`), plus every item in `deferred` or
`waiting_team`, plus every item with `task_id != 0` (with the task's current
status via `st.GetTask`), deduped by item id; `## Memory` — full
`MEMORY.md`; `## Reminder` — act per the role policy, update dossier and
memory, finish with `rocket agent done`.

- [ ] **Step 1: failing test**

```go
func TestBuildBriefingSections(t *testing.T) {
	st := openTestStore(t) // store test helper copied into wake's testutil
	// role "sre" with: message event referencing nothing, deferred item,
	// item with task_id pointing at an in_progress task, unrelated "closed" item.
	out, err := BuildBriefing(st, home, role, events)
	if err != nil {
		t.Fatalf("BuildBriefing: %v", err)
	}
	mustContain(t, out, "## Inbox", "[from user] blocked by X",
		"issue:acme/platform#1 [deferred]", "task:45 [in_work] task #45 in_progress",
		"## Memory", "MEMORY LINE", "rocket agent done")
	if strings.Contains(out, "acme/platform#99") {
		t.Fatalf("closed unrelated item leaked into briefing:\n%s", out)
	}
}
```

- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5:** commit `feat(agentrun): briefing builder for role instances`.

---

### Task 6: the engine (debounce, single instance, done, idle timeout)

**Files:**
- Create: `internal/agentrun/engine.go`, `internal/agentrun/engine_test.go`

**Interfaces consumed:** `Manager.SpawnRole` (Task 4), `BuildBriefing`
(Task 5), `ParseCron` (Task 2), store queries (Task 1).

**Interfaces produced:**
```go
type Spawner interface {
	SpawnRole(ctx context.Context, role store.Agent, briefing string) (store.Session, error)
	Kill(ctx context.Context, id string, cleanup bool) error
}

type Engine struct{ /* st, bus, cfg, spawner, queueWake func(string), now func() time.Time */ }

func New(st *store.Store, b *bus.Bus, cfg *config.Config, sp Spawner, queueWake func(to string)) *Engine
// Notify schedules processing of a role's inbox after the debounce window.
func (e *Engine) Notify(roleID string)
// Done ends the live instance of the role owning sessionID and marks its
// delivered events done. Returns a *session.ValidationError-shaped error when
// the session is not a role instance.
func (e *Engine) Done(ctx context.Context, sessionID string) error
// Tick runs one scheduler pass: snooze expiry, cron, idle timeouts, and
// processing of roles that still have queued events.
func (e *Engine) Tick(ctx context.Context)
func (e *Engine) Run(ctx context.Context)
```

Rules:
- `Notify` starts (or leaves running) one timer per role for
  `cfg.AgentWakeDebounce`; when it fires, `process(role)` runs.
- `process`: skip disabled roles; load queued events (none → return);
  if a live instance exists (session `kind=agent`, id `<role>-run-*`, state
  `spawning|running`) → for each event `st.AddMessage(ToSession: instance,
  Body: "[inbox #<id> <kind>] <text-or-json>")`, publish `message.queued`,
  `queueWake(instance)`, `MarkInboxDelivered`;
  else → `BuildBriefing` + `SpawnRole` + `MarkInboxDelivered`.
- `Done`: mark the role's `delivered` events `done`, `Kill(sessionID, false)`
  (worktree preserved), publish `agent.run_done`; then, if events were queued
  meanwhile, `Notify` again.
- idle timeout: for every live instance whose `ActivityTS` is older than
  `cfg.AgentIdleTimeout` (activity `idle|ready|waiting_input|blocked`), Kill it
  and publish `agent.run_timeout`.
- snooze: `DueSnoozedItems(now)` → enqueue `snooze_expired`
  `{ref, kind, note}`, `ClearAgentItemSnooze`, `Notify`.
- cron: for each enabled role with a parseable `Cron`, keep the next fire time
  in memory (seeded at engine start); when due → enqueue `cron` event +
  `Notify`. An unparseable expression is logged once per role and skipped.

- [ ] **Step 1: failing tests** (fake `Spawner` recording calls; engine built
      with `AgentWakeDebounce: 0` and an injectable `now`):

```go
func TestNotifySpawnsOneInstanceForBatchedEvents(t *testing.T)   // 3 events -> 1 SpawnRole, all events delivered
func TestNotifyInjectsIntoLiveInstance(t *testing.T)             // live sre-run-1 -> no spawn, N messages queued
func TestDoneKillsInstanceAndClosesEvents(t *testing.T)          // events done, Kill(cleanup=false)
func TestTickWakesOnExpiredSnooze(t *testing.T)                  // snooze_expired enqueued once, snooze cleared
func TestTickFiresCron(t *testing.T)                             // cron event enqueued when due
func TestTickKillsIdleInstance(t *testing.T)                     // idle beyond timeout -> Kill + agent.run_timeout
func TestProcessSkipsDisabledRole(t *testing.T)
```

- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS
      (`go test ./internal/agentrun/ -race`).
- [ ] **Step 5:** commit `feat(agentrun): role wake engine and instance lifecycle`.

---

### Task 7: API + CLI wiring

**Files:**
- Modify: `internal/api/server.go` (`Deps.Agents AgentEngine`),
  `internal/api/agents.go`, `internal/api/messages.go`,
  `internal/cli/agent.go`
- Test: `internal/api/agents_test.go`, `internal/api/messages_test.go`,
  `internal/cli/agent_test.go`

**Interfaces produced:**
```go
// AgentEngine is the wake engine as seen by the API (nil in tests/CLI-only builds).
type AgentEngine interface {
	Notify(roleID string)
	Done(ctx context.Context, sessionID string) error
}
```

Changes:
- `handleWakeAgent`: after a successful enqueue, `d.Agents.Notify(a.ID)` when
  `d.Agents != nil`.
- `POST /v1/agents/{id}/done`: caller must be an instance of this role
  (`X-Rocket-Session` → `roleFromSessionID`), otherwise 403; calls
  `d.Agents.Done`; 409 `no_instance` when no live instance exists.
- `handlePostMessage`: when `to` is not a session but names a role, enqueue a
  `message` inbox event `{text: body, from}`, `Notify`, and respond `202` with
  `{"event_id":…,"to":…,"queued":"inbox"}` instead of 404.
- `rocket agent done`: resolves the session from `ROCKET_SESSION_ID`, POSTs
  `/v1/agents/<role>/done`.

- [ ] **Step 1: failing tests**

```go
func TestWakeAgentNotifiesEngine(t *testing.T)        // fake engine records role id
func TestAgentDoneRequiresOwnInstance(t *testing.T)   // foreign session -> 403
func TestAgentDoneCallsEngine(t *testing.T)           // own instance -> 200
func TestPostMessageToRoleEnqueuesInboxEvent(t *testing.T) // 202 + inbox row + Notify
func TestAgentDoneCommandPostsToDaemon(t *testing.T)  // CLI against httptest server
```

- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5:** commit `feat(api,cli): wake notifications, agent done, send to a role`.

---

### Task 8: config + daemon wiring

**Files:**
- Modify: `internal/config/config.go`, `internal/daemon/daemon.go`
- Test: `internal/config/config_test.go`

New config keys (with defaults): `agent_idle_timeout: 15m`,
`agent_wake_debounce: 30s`.

Daemon: build the engine after `mgr`/`q`, pass it as `api.Deps.Agents`, run
`go eng.Run(ctx)`, and call `eng.Notify` for every role returned by
`st.RolesWithQueuedInbox()` at startup (recovery of events enqueued while the
daemon was down) — synchronously before serving, mirroring `q.Recover`.

- [ ] **Step 1: failing test**

```go
func TestAgentRuntimeDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentIdleTimeout != 15*time.Minute || cfg.AgentWakeDebounce != 30*time.Second {
		t.Fatalf("unexpected defaults: %v %v", cfg.AgentIdleTimeout, cfg.AgentWakeDebounce)
	}
}
```

Plus an override case writing `agent_idle_timeout: 5m` into config.yaml.

- [ ] **Step 2:** run → FAIL. **Step 3:** implement config + daemon wiring.
      **Step 4:** run → PASS; `go build ./...`.
- [ ] **Step 5:** commit `feat(daemon): run the role wake engine`.

---

### Task 9: docs + end-to-end verification

**Files:**
- Modify: `docs/10-agents.md` (runtime section: debounce, single instance,
  persistent worktree, briefing, done/idle, snooze/cron, send to a role),
  `docs/03-daemon-api.md` (`POST /v1/agents/{id}/done`, role recipients on
  `POST /v1/messages`), `docs/04-cli.md` (`rocket agent done`, `rocket send
  <role>`), `docs/05-state.md` (new config keys).

- [ ] **Step 1:** `go build ./... && go vet ./... && env -u ROCKET_SOCKET go test ./... -race`
      (`internal/cli` has a pre-existing test that fails when `ROCKET_SOCKET`
      is set in the environment).
- [ ] **Step 2:** manual end-to-end against a scratch `ROCKET_HOME`:
      register a role, `rocket agent wake sre "hi"`, confirm a `sre-run-1`
      tmux session appears with the briefing, `rocket agent state set`
      writes the dossier, `rocket agent done` kills it and leaves the
      worktree in place.
- [ ] **Step 3:** update the docs with what actually shipped.
- [ ] **Step 4:** commit `docs: role runtime (wake engine, instance lifecycle)`.
- [ ] **Step 5:** open the PR referencing feature task-639 / subtask #642.

---

## Self-Review

- Spec coverage: debounce 30s (Task 6), single instance (4, 6), persistent
  worktree (4), briefing (5), system prompt template (3), `rocket agent done`
  (6, 7), idle timeout + `agent.run_timeout` (6), `rocket send <role>` (7),
  snooze + cron (1, 2, 6), live-instance delivery (6), daemon wiring and
  restart recovery (8), docs (9).
- Out of scope here (other subtasks): GitHub event sources and `task_update`
  events (#643), Q&A threads (#644), web/mobile (#645/#646).
