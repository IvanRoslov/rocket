# Agent Task Permissions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Registered persistent agents (`sessions.kind='agent'`) may create, start and edit tasks exactly like the human user; tasks they create are attributed `created_by='agent'`.

**Architecture:** Purely an authorization change in the daemon HTTP layer plus attribution plumbing. Three call sites in `internal/api` branch on `caller.Kind`; the new `agent` value for `created_by` is propagated to the web and mobile clients. No schema migration (the column has no CHECK constraint), no new entities, no privilege flags.

**Tech Stack:** Go 1.x + `net/http` (stdlib mux), SQLite via `internal/store`, React + TypeScript (`web/`, Vitest + Testing Library), React Native TypeScript (`mobile/`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-02-agent-task-permissions-design.md` (v1). Do not deviate; if the spec looks wrong, ask the orchestrator.
- Error strings are exact: `workers may not create tasks`, `only the human user or a registered agent may start a task`.
- `created_by` value for agent-created tasks is exactly `agent`.
- Session kinds in this codebase: `orchestrator`, `worker`, `agent`.
- Follow TDD: failing test first, then minimal implementation. Commit after each green step.
- Comments and docs in the repo are Russian where the surrounding text is Russian (`docs/*.md`); Go comments stay English, matching the files being edited.
- Do not touch `POST /v1/sessions` (worker spawn) authorization.

---

## Task 1: Backend — agent permissions (worker `backend-permissions`)

**Files:**
- Modify: `internal/api/tasks.go:331-346` (`handlePostTask` caller branch)
- Modify: `internal/api/tasks.go:790-800` (`handlePostTaskStart` caller check)
- Modify: `internal/api/auth.go:56-70` (`canWriteTask`)
- Modify: `internal/store/migrations/0001_init.sql:68` (comment only)
- Modify: `docs/10-agents.md`, `docs/12-tasks.md`, `docs/03-daemon-api.md`
- Test: `internal/api/tasks_test.go`, `internal/api/auth_test.go` (create if absent)

**Interfaces:**
- Consumes: `callerSession(r, d.Store) (*store.Session, error)`, `store.Session{ID, Kind, ProjectID, State}`, test helpers `tasksTestDeps(t)`, `newTestServer(t, d)`, `addTestProject(t, d, id)`, `addTestSession(t, d, id, kind, project)`, `bytesReader`, `decodeRepo`, `decodeErr`, `itoa`, `sessionHeader`.
- Produces: HTTP behavior only — no new exported Go symbols. Task 2 relies on the API emitting `"created_by": "agent"` in the task JSON.

- [ ] **Step 1: Write the failing tests for task creation**

Append to `internal/api/tasks_test.go`:

```go
func TestPostTaskAgentCanCreateRootTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "cto", "agent", "proj1")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks", bytesReader([]byte(`{"title":"T","project":"proj1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "cto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["created_by"] != "agent" {
		t.Errorf("created_by = %v, want agent", body["created_by"])
	}
}

func TestPostTaskAgentCanCreateSubtaskOfForeignTask(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "cto", "agent", "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	rootID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks",
		bytesReader([]byte(`{"title":"Sub","project":"proj1","parent_id":`+itoa(rootID)+`}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "cto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

func TestPostTaskWorkerForbidden(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "w-1", "worker", "proj1")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks", bytesReader([]byte(`{"title":"T","project":"proj1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "w-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Message != "workers may not create tasks" {
		t.Errorf("message = %q, want %q", eb.Error.Message, "workers may not create tasks")
	}
}
```

Note: the existing `TestPostTaskAgentRootTaskForbidden` actually exercises an *orchestrator*. Rename it to `TestPostTaskOrchestratorRootTaskForbidden` in this step; its body stays unchanged.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestPostTaskAgent|TestPostTaskWorkerForbidden' -v`
Expected: FAIL — agent cases return 403 `only orchestrators may create tasks`; the worker case fails on the message assertion.

- [ ] **Step 3: Implement the caller branch in `handlePostTask`**

Replace the block at `internal/api/tasks.go:331-346`:

```go
	createdBy := "user"
	if caller != nil {
		switch caller.Kind {
		case "agent":
			// A registered persistent agent has the same task rights as the
			// human user; its session id is attribution, not authentication.
			createdBy = "agent"
		case "orchestrator":
			createdBy = "orchestrator"
			if req.ParentID == 0 {
				writeErr(w, http.StatusForbidden, "forbidden", "agents may only create subtasks")
				return
			}
			if parent.SessionID != caller.ID {
				writeErr(w, http.StatusForbidden, "forbidden", "parent task does not belong to caller")
				return
			}
		default:
			writeErr(w, http.StatusForbidden, "forbidden", "workers may not create tasks")
			return
		}
	}
```

- [ ] **Step 4: Run the creation tests**

Run: `go test ./internal/api/ -run TestPostTask -v`
Expected: PASS, including the pre-existing orchestrator tests.

- [ ] **Step 5: Commit**

```bash
git add internal/api/tasks.go internal/api/tasks_test.go
git commit -m "api: let registered agents create tasks"
```

- [ ] **Step 6: Write the failing test for task start**

Append to `internal/api/tasks_test.go`:

```go
func TestPostTaskStartAgentAllowed(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "cto", "agent", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "Add login page", ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/tasks/"+itoa(id)+"/start", nil)
	req.Header.Set(sessionHeader, "cto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}
```

Also update `TestPostTaskStartAgentCallerForbidden`: rename to `TestPostTaskStartOrchestratorForbidden` (it uses an orchestrator session) and assert the new message:

```go
	eb := decodeErr(t, resp)
	if eb.Error.Message != "only the human user or a registered agent may start a task" {
		t.Errorf("message = %q, want the agent-aware message", eb.Error.Message)
	}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `go test ./internal/api/ -run TestPostTaskStart -v`
Expected: FAIL — agent gets 403; the renamed test fails on the message assertion.

- [ ] **Step 8: Implement the start check**

At `internal/api/tasks.go:794`, replace the caller rejection with:

```go
	if caller != nil && caller.Kind != "agent" {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user or a registered agent may start a task")
		return
	}
```

- [ ] **Step 9: Run the start tests**

Run: `go test ./internal/api/ -run TestPostTaskStart -v`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/api/tasks.go internal/api/tasks_test.go
git commit -m "api: let registered agents start tasks"
```

- [ ] **Step 11: Write the failing test for `canWriteTask`**

Add to `internal/api/tasks_test.go` (an end-to-end check through the PATCH route is preferred over a unit test on the unexported helper, because it also proves the route is wired):

```go
func TestPatchTaskByAgentAllowed(t *testing.T) {
	d := tasksTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "cto", "agent", "proj1")

	id, err := d.Store.AddTask(store.Task{Title: "T", ProjectID: "proj1", CreatedBy: "agent"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/v1/tasks/"+itoa(id),
		bytesReader([]byte(`{"description":"updated by agent"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sessionHeader, "cto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
```

If the update route in this codebase is not `PATCH /v1/tasks/{id}` with a `description` field, read `internal/api/tasks.go` `registerTaskRoutes` and adapt the method/path/body to the real one — the assertion (an agent may write to a task it does not own) must stay.

- [ ] **Step 12: Run it to verify it fails**

Run: `go test ./internal/api/ -run TestPatchTaskByAgentAllowed -v`
Expected: FAIL with 403.

- [ ] **Step 13: Implement in `canWriteTask`**

In `internal/api/auth.go`, right after the `caller == nil` early return:

```go
	// A registered persistent agent has the same rights as the human user.
	if caller.Kind == "agent" {
		return true
	}
```

Update the doc comment above the function to mention agents.

- [ ] **Step 14: Run the full API suite**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 15: Commit**

```bash
git add internal/api/auth.go internal/api/tasks_test.go
git commit -m "api: agents may write to tasks like the human user"
```

- [ ] **Step 16: Update the migration comment and docs**

`internal/store/migrations/0001_init.sql:68` — change the trailing comment to `-- user|orchestrator|agent`. Do not alter the DDL; there is no CHECK constraint and no migration is needed.

`docs/10-agents.md`, in the «Постоянные агенты» section, add a subsection:

```markdown
### Права

Постоянный агент в правах на задачи приравнен к человеку: создаёт задачи
(корневые и подзадачи, в любом проекте), запускает их (`rocket up`,
`rocket task start`) и редактирует. Задачи, созданные агентом, получают
`created_by='agent'`.

Чего агент не может: спавнить воркеров (`POST /v1/sessions` — только живой
оркестратор, у которого есть контекст фичи, ветки и карточки подзадачи).

Идентификатор сессии приходит из `ROCKET_SESSION_ID` и является атрибуцией,
а не аутентификацией: запуск с `env -u ROCKET_SESSION_ID` выполняет команду
от имени человека и теряет авторство. Реальный контроль доступа потребовал бы
подписанных токенов сессий — это отдельная задача.
```

`docs/12-tasks.md` и `docs/03-daemon-api.md` — в описаниях `POST /v1/tasks` и
`POST /v1/tasks/{id}/start` перечислить, кому что разрешено, и привести новые
тексты 403 (`workers may not create tasks`,
`only the human user or a registered agent may start a task`). Найти места:
`grep -n "only orchestrators may create tasks\|only the human user may start" docs/`.

- [ ] **Step 17: Verify the whole build and test suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 18: Commit and open the PR**

```bash
git add internal/store/migrations/0001_init.sql docs/
git commit -m "docs: document task permissions of persistent agents"
```

Open the PR against `main` and report the number to the orchestrator.

---

## Task 2: Clients — render `created_by='agent'` (worker `ui-created-by-agent`)

**Files:**
- Modify: `web/src/lib/types.ts:280` (`TaskCreatedBy`)
- Modify: `web/src/screens/task/TaskScreen.tsx:110` (author label)
- Modify: `mobile/src/api/types.ts:119` (`created_by` union)
- Test: `web/src/screens/task/Task.test.tsx`

**Interfaces:**
- Consumes: the task JSON from `GET /v1/tasks/{id}`, whose `created_by` is now one of `user | orchestrator | agent` (produced by Task 1). No code dependency — the two tasks may run in parallel.
- Produces: nothing consumed by other tasks.

- [ ] **Step 1: Write the failing test**

In `web/src/screens/task/Task.test.tsx`, copy the nearest existing test that renders a task and asserts on the header (see the fixtures around lines 334 and 399 which set `created_by`), and add:

```tsx
it('shows the agent as the task author', async () => {
  // ...same setup as the neighbouring test, with created_by: 'agent'
  render(<TaskScreen />)
  expect(await screen.findByText(/created by agent/)).toBeInTheDocument()
})
```

Match the surrounding tests' setup idiom (MSW handler override or fixture) rather than inventing a new one.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npm test -- Task.test.tsx`
Expected: FAIL — the component renders `created by orchestrator` for `agent`, and TypeScript rejects `created_by: 'agent'`.

- [ ] **Step 3: Widen the type**

`web/src/lib/types.ts:280`:

```ts
export type TaskCreatedBy = 'user' | 'orchestrator' | 'agent'
```

`mobile/src/api/types.ts:119`:

```ts
  created_by: 'user' | 'orchestrator' | 'agent'
```

- [ ] **Step 4: Render all three values**

`web/src/screens/task/TaskScreen.tsx:110` — replace the two-way ternary with an explicit map so an unknown future value is not silently labelled `orchestrator`:

```tsx
const authorLabel: Record<TaskCreatedBy, string> = {
  user: 'you',
  orchestrator: 'orchestrator',
  agent: 'agent',
}
```

and use `{authorLabel[task.created_by] ?? task.created_by}` in the `<span>`.

- [ ] **Step 5: Run the tests**

Run: `cd web && npm test -- Task.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck and lint both clients**

Run: `cd web && npm run typecheck && npm run lint` then the mobile equivalents (check `mobile/package.json` for the exact script names; if only `tsc` exists, run `npx tsc --noEmit`).
Expected: clean.

- [ ] **Step 7: Commit and open the PR**

```bash
git add web/src mobile/src
git commit -m "web,mobile: show agent as a task author"
```

Open the PR against `main` and report the number to the orchestrator.
