# Persistent Agents — Web Dashboard (Agents screen) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an Agents screen to the rocket web dashboard: roles list, create/edit
form, role card (inbox, dossier, memory, runs, Q&A threads, embedded terminal,
wake/enable/disable) and role "awaiting answer" badges on the projects list.

**Architecture:** Pure additive frontend work in `web/` over the already-merged
`/v1/agents` API (internal/api/agents.go, agent_questions.go), plus one small
backend addition (`GET/PUT /v1/agents/{id}/memory`) because the spec's memory
viewer/editor has no endpoint yet. Two new routes under the project scope
(`/p/:projectId/agents` and `/p/:projectId/agents/:roleId`) inside AppShell.
Data flows through the existing `api` client + react-query hooks in
`lib/queries.ts`, live-refreshed by the existing SSE singleton (`lib/sse.ts`)
once `agent.*` event types are registered. Q&A threads reuse the task thread
component by extracting its presentational core.

**Tech Stack:** Go 1.x (net/http std mux) for the memory endpoint; TypeScript +
React 19 + react-router-dom 7 + @tanstack/react-query 5 + msw 2 + vitest 4 for
the dashboard.

## Global Constraints

- Data source is the daemon's public API only (docs/11-dashboard.md): REST + SSE.
- Role ids match `^[a-z0-9-]+$`; role instance sessions are named `<role>-run-<n>`
  and have `kind: "agent"` (docs/10-agents.md).
- Identifiers, comments and commit messages in English; UI copy in English, matching
  the existing screens.
- Runs journal comes from `GET /v1/sessions?kind=agent&all=true`, filtered by the
  `<role>-run-` id prefix — no new runs endpoint.
- `web/dist/index.html` is a tracked placeholder: never commit a real build.
- Every new component gets a vitest test rendered against msw handlers, following
  `web/src/screens/**/**.test.tsx` patterns.
- Run `cd web && npx tsc -b && npm test` before each commit; `go test ./internal/api/...`
  for the backend task.

---

## File Structure

**Backend (Task 1 only):**
- Modify `internal/api/agents.go` — register + implement `GET/PUT /v1/agents/{id}/memory`.
- Modify `internal/api/agents_test.go` — endpoint tests.
- Modify `internal/roles/roles.go` — `ReadMemoryIndex` / `WriteMemoryIndex` helpers.
- Modify `internal/roles/roles_test.go`.
- Modify `docs/10-agents.md`, `docs/03-daemon-api.md` — document the endpoint.

**Frontend:**
- Modify `web/src/lib/types.ts` — `Agent`, `AgentInboxEvent`, `AgentItem`,
  `AgentQuestion`, `AgentMemory`.
- Modify `web/src/lib/queries.ts` — agent queries/mutations + `agent.*` invalidation.
- Modify `web/src/lib/sse.ts` — register `agent.*` event types.
- Modify `web/src/mocks/fixtures.ts`, `web/src/mocks/handlers.ts` — agent fixtures/handlers.
- Create `web/src/components/QuestionThreadView.tsx` — presentational thread
  (extracted from `QuestionThread.tsx`), shared by task and role threads.
- Modify `web/src/components/QuestionThread.tsx` — task-bound wrapper over the view.
- Create `web/src/components/AgentQuestionThread.tsx` — role-bound wrapper.
- Create `web/src/screens/agents/AgentsScreen.tsx` + `AgentCard.tsx` — roles list.
- Create `web/src/screens/agents/AgentFormModal.tsx` — create/edit role.
- Create `web/src/screens/agents/AgentScreen.tsx` — role card shell (header,
  actions, tabs, terminal).
- Create `web/src/screens/agents/InboxTab.tsx`, `DossierTab.tsx`, `MemoryTab.tsx`,
  `RunsTab.tsx`, `AgentQuestionsTab.tsx`.
- Create `web/src/screens/agents/agents.css`.
- Create `web/src/screens/agents/Agents.test.tsx`, `AgentScreen.test.tsx`.
- Modify `web/src/routes.tsx`, `web/src/components/AppShell.tsx` — routes + nav.
- Modify `web/src/screens/projects/ProjectCard.tsx` (+ `ProjectsScreen.test.tsx`) —
  role awaiting-answer badge.
- Modify `web/src/screens/kanban/KanbanScreen.tsx` — "Agents" link in the board header.
- Modify `docs/11-dashboard.md` — document the screen.

---

### Task 1: Backend — role memory endpoint

The spec (docs/10-agents.md, "Дашборд") lists `GET /v1/agents/{id}/memory`, but
`internal/api/agents.go` has no such route: the memory viewer/editor has nothing
to call. Add it — the shape below is a fixed contract, the mobile worker consumes
the same GET read-only.

**Files:**
- Modify: `internal/roles/roles.go`
- Test: `internal/roles/roles_test.go`
- Modify: `internal/api/agents.go`
- Test: `internal/api/agents_test.go`
- Modify: `docs/10-agents.md`, `docs/03-daemon-api.md`

**Interfaces:**
- Consumes: `roles.Dir/MemoryDir/MemoryIndexPath` (existing).
- Produces:
  - `roles.MemoryFile{Name string; Size int64; UpdatedAt int64; Body string}`
  - `roles.ReadMemory(home, id string) (index string, files []MemoryFile, err error)`
  - `roles.WriteMemoryFile(home, id, name, body string) error` (name validated)
  - `roles.ValidMemoryFileName(name string) bool`
  - `GET /v1/agents/{id}/memory` -> `{"path":"…","index":"…","files":[{"name","size","updated_at","body"}]}`
  - `PUT /v1/agents/{id}/memory` `{"file":"platform.md","body":"…"}` (file defaults
    to `MEMORY.md`) -> the same shape, 200; `400 {code:"invalid_file"}` on a bad name.

- [ ] **Step 1: Write the failing roles tests**

```go
func TestReadMemoryReturnsIndexAndFactFiles(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(MemoryDir(home, "sre"), "platform.md"), []byte("fact"), 0600); err != nil {
		t.Fatalf("write fact: %v", err)
	}

	index, files, err := ReadMemory(home, "sre")
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if index == "" {
		t.Fatal("want the seeded MEMORY.md index, got empty string")
	}
	if len(files) != 1 {
		t.Fatalf("want only the fact file (MEMORY.md excluded), got %v", files)
	}
	if files[0].Name != "platform.md" || files[0].Body != "fact" || files[0].Size != 4 {
		t.Fatalf("unexpected file entry: %+v", files[0])
	}
}

func TestWriteMemoryFileRoundTrips(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := WriteMemoryFile(home, "sre", "MEMORY.md", "- [Platform](platform.md)\n"); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := WriteMemoryFile(home, "sre", "platform.md", "how it deploys"); err != nil {
		t.Fatalf("write fact: %v", err)
	}
	index, files, err := ReadMemory(home, "sre")
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if index != "- [Platform](platform.md)\n" {
		t.Fatalf("index not persisted, got %q", index)
	}
	if len(files) != 1 || files[0].Body != "how it deploys" {
		t.Fatalf("fact file not persisted: %+v", files)
	}
}

func TestWriteMemoryFileRejectsTraversal(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, name := range []string{"../role.md", "a/b.md", "", ".", "..", "notes.txt", "/etc/passwd"} {
		if err := WriteMemoryFile(home, "sre", name, "x"); err == nil {
			t.Fatalf("want an error for %q, got nil", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/roles/ -run Memory -v`
Expected: FAIL, `undefined: ReadMemory`.

- [ ] **Step 3: Implement the roles helpers**

```go
// MemoryFile is one fact file of a role's file memory. Bodies are inlined:
// role memory is a handful of short markdown notes, and the dashboard and the
// mobile app both render all of them at once.
type MemoryFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updated_at"`
	Body      string `json:"body"`
}

// memoryFileName matches an acceptable memory file name: a base name only, no
// path separators, markdown.
var memoryFileName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.md$`)

// ValidMemoryFileName reports whether name may be written into a role's memory
// directory. It is deliberately strict — the name comes from an HTTP body, and
// the memory dir sits next to role.md in the role's home.
func ValidMemoryFileName(name string) bool {
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return false
	}
	return memoryFileName.MatchString(name)
}

// ReadMemory returns the role's memory index (MEMORY.md) and the fact files
// beside it, sorted by name. Sub-directories are ignored.
func ReadMemory(home, id string) (string, []MemoryFile, error) { … }

// WriteMemoryFile creates or replaces one file of the role's memory.
func WriteMemoryFile(home, id, name, body string) error { … }
```

`WriteMemoryFile` rejects an invalid name with a sentinel error
(`ErrInvalidMemoryFile`), then — belt and braces — resolves the joined path and
verifies it is still inside `MemoryDir(home, id)` before writing 0600.

- [ ] **Step 4: Run the roles tests**

Run: `go test ./internal/roles/ -v`
Expected: PASS.

- [ ] **Step 5: Write the failing API tests**

Cover, in `internal/api/agents_test.go` (reusing that file's existing helpers):
GET returns `{path,index,files}` for a fresh role; PUT with no `file` rewrites
`MEMORY.md` and the following GET shows it; PUT `{"file":"platform.md"}` creates
a fact file that shows up in `files` with its body; PUT `{"file":"../role.md"}`
-> `400 invalid_file`; GET/PUT on an unknown role -> `404 agent_not_found`.

- [ ] **Step 6: Run the API tests to verify they fail**

Run: `go test ./internal/api/ -run TestAgentMemory -v`
Expected: FAIL (404 from the unregistered route).

- [ ] **Step 7: Implement the endpoint**

Register in `registerAgentRoutes` next to inbox/items:

```go
	mux.HandleFunc("GET /v1/agents/{id}/memory", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentMemory(w, r, d)
	})
	mux.HandleFunc("PUT /v1/agents/{id}/memory", func(w http.ResponseWriter, r *http.Request) {
		handlePutAgentMemory(w, r, d)
	})
```

```go
// agentMemoryResponse is the JSON shape of a role's file memory: the MEMORY.md
// index plus the fact files beside it, bodies inlined.
type agentMemoryResponse struct {
	Path  string             `json:"path"`
	Index string             `json:"index"`
	Files []roles.MemoryFile `json:"files"`
}
```

`handleGetAgentMemory` -> `lookupAgent` + `writeAgentMemory`.
`handlePutAgentMemory` -> `lookupAgent`, decode `{file, body}`, default `file` to
`MEMORY.md`, `roles.WriteMemoryFile`; map `roles.ErrInvalidMemoryFile` to
`400 invalid_file`, anything else to `internal_error`; then `writeAgentMemory`.

- [ ] **Step 8: Run the API tests**

Run: `go test ./internal/api/ ./internal/roles/`
Expected: PASS.

- [ ] **Step 9: Document the endpoint**

Add the two routes with the exact JSON shape above to the agents section of
`docs/03-daemon-api.md`, and a short bullet in `docs/10-agents.md` next to the
role home-directory bullet.

- [ ] **Step 10: Commit**

```bash
git add internal/roles internal/api docs/10-agents.md docs/03-daemon-api.md
git commit -m "api: role memory endpoint (GET/PUT /v1/agents/{id}/memory)"
```

---

### Task 2: Frontend types, queries, SSE and mocks

The data layer every later task builds on. No UI yet — the test asserts the
hooks against msw.

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/queries.ts`
- Modify: `web/src/lib/sse.ts`
- Modify: `web/src/mocks/fixtures.ts`
- Modify: `web/src/mocks/handlers.ts`
- Test: `web/src/lib/queries.test.tsx` (existing file — append)

**Interfaces:**
- Consumes: `api` from `./api`; the daemon shapes in `internal/api/agents.go`
  (`agentResponse`, `inboxEventResponse`, `agentItemResponse`) and
  `internal/api/agent_questions.go` (`agentQuestionResponse`).
- Produces (used by Tasks 3-8):
  - types `Agent`, `AgentInboxEvent`, `AgentItem`, `AgentQuestion`, `AgentMemory`
  - `useAgents(projectId?: string): UseQueryResult<Agent[]>`
  - `useAgent(id?: string): UseQueryResult<Agent>`
  - `useAgentInbox(id?: string, status?: string): UseQueryResult<AgentInboxEvent[]>`
  - `useAgentItems(id?: string, state?: string): UseQueryResult<AgentItem[]>`
  - `useAgentMemory(id?: string): UseQueryResult<AgentMemory>`
  - `useAgentQuestions(id?: string): UseQueryResult<AgentQuestion[]>`
  - `useCreateAgent()`, `useUpdateAgent()`, `useDeleteAgent()`,
    `useSetAgentEnabled()`, `useWakeAgent()`, `useUpdateAgentMemory()`,
    `useAskAgent(roleId)`, `useReplyAgentQuestion()`, `useAnswerAgentQuestion()`

- [ ] **Step 1: Add the types**

Append to `web/src/lib/types.ts`:

```ts
// ---------------------------------------------------------------------------
// Agent roles — internal/api/agents.go, internal/api/agent_questions.go
// ---------------------------------------------------------------------------

export interface AgentSubscription {
  repo: string
  labels?: string[]
  mention_only?: boolean
}

/** A registered role. `prompt` is only present on `GET /v1/agents/{id}`. */
export interface Agent {
  id: string
  project: string
  prompt_path: string
  prompt?: string
  subscriptions: AgentSubscription[]
  cron: string
  agent: string
  enabled: boolean
  inbox_queued: number
  items: number
  open_questions: number
  awaiting_user: number
  created_at: number
  updated_at: number
}

export type AgentInboxKind =
  | 'message'
  | 'issue_opened'
  | 'issue_comment'
  | 'task_update'
  | 'snooze_expired'
  | 'cron'
  | 'question'
  | 'terminal_opened'

export interface AgentInboxEvent {
  id: number
  kind: AgentInboxKind
  /** Kind-specific JSON: `{text, from}` for `message`, `{repo, number, ...}`
   * for issue events, `{task_id, title, from, to}` for `task_update`. */
  payload: Record<string, unknown>
  status: 'queued' | 'delivered' | 'done'
  created_at: number
  updated_at: number
}

export interface AgentItem {
  id: number
  kind: 'issue' | 'task' | 'ping'
  ref: string
  state: string
  note: string
  task_id: number
  snooze_until: number
  created_at: number
  updated_at: number
}

/** Role Q&A thread. Mirrors `Question` with `role_id` in place of the task and
 * `whose_turn: 'user' | 'role'`. */
export interface AgentQuestion {
  id: number
  role_id: string
  ordinal: number
  asked_by: string
  body: string
  context?: string
  status: 'open' | 'resolved'
  resolution?: string
  whose_turn?: 'user' | 'role'
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

/** One fact file of a role's file memory (body inlined). */
export interface AgentMemoryFile {
  name: string
  size: number
  updated_at: number
  body: string
}

/** `GET /v1/agents/{id}/memory`: the MEMORY.md index plus the fact files
 * beside it. `PUT` writes one file at a time (`file` defaults to MEMORY.md). */
export interface AgentMemory {
  path: string
  index: string
  files: AgentMemoryFile[]
}
```

- [ ] **Step 2: Write the failing hook tests**

Append to `web/src/lib/queries.test.tsx`, following that file's existing
`renderHook` + wrapper helper:

```tsx
describe('agent queries', () => {
  it('useAgents unwraps the bare array from GET /v1/agents', async () => {
    const { result } = renderHook(() => useAgents('billing'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.map((a) => a.id)).toContain('sre')
  })

  it('useAgentInbox returns the role inbox events', async () => {
    const { result } = renderHook(() => useAgentInbox('sre'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.[0].kind).toBe('message')
  })

  it('useAgentQuestions unwraps the {questions:[]} envelope', async () => {
    const { result } = renderHook(() => useAgentQuestions('sre'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.[0].role_id).toBe('sre')
  })
})
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/queries.test.tsx`
Expected: FAIL — `useAgents is not defined`.

- [ ] **Step 4: Add the fixtures**

Append to `web/src/mocks/fixtures.ts` (match the file's existing export style):

```ts
export const agents: Agent[] = [
  {
    id: 'sre',
    project: 'billing',
    prompt_path: '/home/u/.rocket/agents/sre/role.md',
    subscriptions: [{ repo: 'acme/platform', labels: ['bug'], mention_only: false }],
    cron: '0 * * * *',
    agent: 'claude-code',
    enabled: true,
    inbox_queued: 2,
    items: 3,
    open_questions: 1,
    awaiting_user: 1,
    created_at: 1754000000,
    updated_at: 1754003600,
  },
  {
    id: 'triage',
    project: 'billing',
    prompt_path: '/home/u/.rocket/agents/triage/role.md',
    subscriptions: [],
    cron: '',
    agent: 'claude-code',
    enabled: false,
    inbox_queued: 0,
    items: 0,
    open_questions: 0,
    awaiting_user: 0,
    created_at: 1754000000,
    updated_at: 1754000000,
  },
]

export const agentPrompt = '# SRE\n\nTriage platform issues.\n'

export const agentInbox: AgentInboxEvent[] = [
  {
    id: 1,
    kind: 'message',
    payload: { text: 'blocked by X', from: 'billing-v2-orch' },
    status: 'queued',
    created_at: 1754003000,
    updated_at: 1754003000,
  },
  {
    id: 2,
    kind: 'issue_opened',
    payload: { repo: 'acme/platform', number: 42, title: 'DB migration stuck' },
    status: 'done',
    created_at: 1754002000,
    updated_at: 1754002500,
  },
]

export const agentItems: AgentItem[] = [
  {
    id: 1,
    kind: 'issue',
    ref: 'acme/platform#42',
    state: 'taken',
    note: 'task created',
    task_id: 12,
    snooze_until: 0,
    created_at: 1754002000,
    updated_at: 1754002600,
  },
  {
    id: 2,
    kind: 'issue',
    ref: 'acme/platform#43',
    state: 'deferred',
    note: 'waiting for the DB migration',
    task_id: 0,
    snooze_until: 1754500000,
    created_at: 1754001000,
    updated_at: 1754001000,
  },
]

export const agentMemory: AgentMemory = {
  path: '/home/u/.rocket/agents/sre/memory',
  index: '- [Platform](platform.md) — how the platform deploys\n',
  files: [
    { name: 'platform.md', size: 14, updated_at: 1754003000, body: 'how it deploys' },
  ],
}

export const agentQuestions: AgentQuestion[] = [
  {
    id: 91,
    role_id: 'sre',
    ordinal: 1,
    asked_by: 'sre-run-3',
    body: 'Should I close acme/platform#42 now?',
    status: 'open',
    whose_turn: 'user',
    asked_at: 1754003000,
    messages: [],
  },
]

export const agentRuns: Session[] = [
  {
    id: 'sre-run-3',
    kind: 'agent',
    project_id: 'billing',
    repo_id: 'api',
    feature_slug: '',
    agent: 'claude-code',
    branch: 'agent/sre',
    worktree_path: '/home/u/.rocket/worktrees/api/sre-agent',
    tmux_name: 'sre-run-3',
    state: 'running',
    activity: 'active',
    created_at: 1754003000,
    updated_at: 1754003100,
  },
  {
    id: 'sre-run-2',
    kind: 'agent',
    project_id: 'billing',
    repo_id: 'api',
    feature_slug: '',
    agent: 'claude-code',
    branch: 'agent/sre',
    worktree_path: '/home/u/.rocket/worktrees/api/sre-agent',
    tmux_name: 'sre-run-2',
    state: 'done',
    created_at: 1754000000,
    updated_at: 1754001000,
  },
]
```

Import the new types at the top of the file alongside the existing imports.

- [ ] **Step 5: Add the msw handlers**

In `web/src/mocks/handlers.ts`, add mutable state next to the existing
`sessionsState` block and handlers next to the task ones:

```ts
let agentsState: Agent[] = agents.map((a) => ({ ...a }))
let agentMemoryState: AgentMemory = { ...agentMemory, files: [...agentMemory.files] }

export function resetAgents(): void {
  agentsState = agents.map((a) => ({ ...a }))
  agentMemoryState = { ...agentMemory, files: [...agentMemory.files] }
}
```

```ts
  http.get('/v1/agents', ({ request }) => {
    const project = new URL(request.url).searchParams.get('project')
    return HttpResponse.json(
      project ? agentsState.filter((a) => a.project === project) : agentsState,
    )
  }),
  http.post('/v1/agents', async ({ request }) => {
    const body = (await request.json()) as { id: string; project: string; prompt?: string }
    const created: Agent = {
      ...agentsState[0],
      id: body.id,
      project: body.project,
      prompt: body.prompt ?? '',
      subscriptions: [],
      cron: '',
      inbox_queued: 0,
      items: 0,
      open_questions: 0,
      awaiting_user: 0,
      enabled: true,
    }
    agentsState = [...agentsState, created]
    return HttpResponse.json(created, { status: 201 })
  }),
  http.get('/v1/agents/:id', ({ params }) => {
    const found = agentsState.find((a) => a.id === params.id)
    if (!found) {
      return HttpResponse.json(
        { error: { code: 'agent_not_found', message: 'agent not found' } },
        { status: 404 },
      )
    }
    return HttpResponse.json({ ...found, prompt: agentPrompt })
  }),
  http.patch('/v1/agents/:id', async ({ params, request }) => {
    const body = (await request.json()) as Partial<Agent>
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, ...body } : a))
    const updated = agentsState.find((a) => a.id === params.id)!
    return HttpResponse.json({ ...updated, prompt: agentPrompt })
  }),
  http.delete('/v1/agents/:id', ({ params }) => {
    agentsState = agentsState.filter((a) => a.id !== params.id)
    return HttpResponse.json({ status: 'deleted' })
  }),
  http.post('/v1/agents/:id/enable', ({ params }) => {
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, enabled: true } : a))
    return HttpResponse.json(agentsState.find((a) => a.id === params.id))
  }),
  http.post('/v1/agents/:id/disable', ({ params }) => {
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, enabled: false } : a))
    return HttpResponse.json(agentsState.find((a) => a.id === params.id))
  }),
  http.post('/v1/agents/:id/wake', () => HttpResponse.json({ event_id: 7, kind: 'message' }, { status: 202 })),
  http.get('/v1/agents/:id/inbox', () => HttpResponse.json(agentInbox)),
  http.get('/v1/agents/:id/items', ({ request }) => {
    const state = new URL(request.url).searchParams.get('state')
    return HttpResponse.json(state ? agentItems.filter((i) => i.state === state) : agentItems)
  }),
  http.get('/v1/agents/:id/memory', () => HttpResponse.json(agentMemoryState)),
  http.put('/v1/agents/:id/memory', async ({ request }) => {
    const req = (await request.json()) as { file?: string; body: string }
    const file = req.file ?? 'MEMORY.md'
    if (file === 'MEMORY.md') {
      agentMemoryState = { ...agentMemoryState, index: req.body }
    } else {
      const files = agentMemoryState.files.filter((f) => f.name !== file)
      agentMemoryState = {
        ...agentMemoryState,
        files: [...files, { name: file, size: req.body.length, updated_at: 1754004000, body: req.body }],
      }
    }
    return HttpResponse.json(agentMemoryState)
  }),
  http.get('/v1/agents/:id/questions', () => HttpResponse.json({ questions: agentQuestions })),
  http.post('/v1/agents/:id/questions', async ({ params, request }) => {
    const body = (await request.json()) as { body: string; context?: string }
    return HttpResponse.json(
      {
        id: 92,
        role_id: params.id as string,
        ordinal: 2,
        asked_by: '',
        body: body.body,
        context: body.context,
        status: 'open',
        whose_turn: 'role',
        asked_at: 1754004000,
        messages: [],
      },
      { status: 201 },
    )
  }),
  http.post('/v1/agent-questions/:id/reply', () =>
    HttpResponse.json({ ...agentQuestions[0], whose_turn: 'role' }, { status: 201 }),
  ),
  http.post('/v1/agent-questions/:id/answer', () =>
    HttpResponse.json({ ...agentQuestions[0], status: 'resolved', whose_turn: undefined }),
  ),
```

The existing `GET /v1/sessions` handler must also serve the `kind=agent&all=true`
query used by the runs journal — extend it so `kind` filters
`[...sessionsState, ...agentRuns]` rather than `sessionsState` alone.

- [ ] **Step 6: Add the query hooks**

Append to `web/src/lib/queries.ts` (imports extended with the new types):

```ts
// ---------------------------------------------------------------------------
// Agent roles (docs/10-agents.md «Роли»)
// ---------------------------------------------------------------------------

/** `GET /v1/agents[?project=]` — bare array of roles (no prompt body). */
export function useAgents(projectId?: string): UseQueryResult<Agent[]> {
  return useQuery({
    queryKey: ['agents', projectId ?? 'all'],
    queryFn: () =>
      api.get<Agent[]>(`/v1/agents${projectId ? `?project=${encodeURIComponent(projectId)}` : ''}`),
  })
}

/** `GET /v1/agents/{id}` — the role WITH its `prompt` body (the list omits it). */
export function useAgent(id?: string): UseQueryResult<Agent> {
  return useQuery({
    queryKey: ['agent', id],
    queryFn: () => api.get<Agent>(`/v1/agents/${id}`),
    enabled: !!id,
  })
}

export function useAgentInbox(id?: string, status?: string): UseQueryResult<AgentInboxEvent[]> {
  return useQuery({
    queryKey: ['agent', id, 'inbox', status ?? 'all'],
    queryFn: () =>
      api.get<AgentInboxEvent[]>(
        `/v1/agents/${id}/inbox${status ? `?status=${encodeURIComponent(status)}` : ''}`,
      ),
    enabled: !!id,
  })
}

export function useAgentItems(id?: string, state?: string): UseQueryResult<AgentItem[]> {
  return useQuery({
    queryKey: ['agent', id, 'items', state ?? 'all'],
    queryFn: () =>
      api.get<AgentItem[]>(
        `/v1/agents/${id}/items${state ? `?state=${encodeURIComponent(state)}` : ''}`,
      ),
    enabled: !!id,
  })
}

export function useAgentMemory(id?: string): UseQueryResult<AgentMemory> {
  return useQuery({
    queryKey: ['agent', id, 'memory'],
    queryFn: () => api.get<AgentMemory>(`/v1/agents/${id}/memory`),
    enabled: !!id,
  })
}

export function useAgentQuestions(id?: string): UseQueryResult<AgentQuestion[]> {
  return useQuery({
    queryKey: ['agent', id, 'questions'],
    queryFn: async () => {
      const res = await api.get<{ questions: AgentQuestion[] }>(`/v1/agents/${id}/questions`)
      return res.questions
    },
    enabled: !!id,
  })
}

export interface AgentFormValues {
  id: string
  project: string
  prompt: string
  subscriptions: AgentSubscription[]
  cron: string
  agent: string
}

/** `POST /v1/agents` -> bare agentResponse (201). */
export function useCreateAgent(): UseMutationResult<Agent, Error, AgentFormValues> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<Agent>('/v1/agents', payload),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['agents'] }),
  })
}

/** `PATCH /v1/agents/{id}` — every field optional; `prompt` rewrites role.md. */
export function useUpdateAgent(): UseMutationResult<
  Agent,
  Error,
  { id: string } & Partial<Omit<AgentFormValues, 'id' | 'project'>> & { enabled?: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }) => api.patch<Agent>(`/v1/agents/${id}`, body),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
    },
  })
}

export function useDeleteAgent(): UseMutationResult<void, Error, string> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id) => api.del<void>(`/v1/agents/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['agents'] }),
  })
}

/** `POST /v1/agents/{id}/enable|disable` -> bare agentResponse. */
export function useSetAgentEnabled(): UseMutationResult<
  Agent,
  Error,
  { id: string; enabled: boolean }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }) => api.post<Agent>(`/v1/agents/${id}/${enabled ? 'enable' : 'disable'}`),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
    },
  })
}

/**
 * `POST /v1/agents/{id}/wake` `{kind?, text?, from?}` -> `{event_id, kind}`
 * (202). Enqueues an inbox event and notifies the wake engine: a live
 * instance receives it as a message, otherwise one is spawned after the
 * daemon's debounce window (30s by default, docs/10-agents.md).
 */
export function useWakeAgent(): UseMutationResult<
  { event_id: number; kind: string },
  Error,
  { id: string; kind?: AgentInboxKind; text?: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, kind, text }) =>
      api.post<{ event_id: number; kind: string }>(`/v1/agents/${id}/wake`, { kind, text }),
    onSuccess: (_data, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', id] })
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    },
  })
}

/** `PUT /v1/agents/{id}/memory` `{file?, body}` — writes ONE memory file;
 * `file` defaults to `MEMORY.md`. The daemon validates the name (base name,
 * `.md`, no separators) and rejects anything else with `400 invalid_file`. */
export function useUpdateAgentMemory(): UseMutationResult<
  AgentMemory,
  Error,
  { id: string; file?: string; body: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, file, body }) => api.put<AgentMemory>(`/v1/agents/${id}/memory`, { file, body }),
    onSuccess: (_data, { id }) => queryClient.invalidateQueries({ queryKey: ['agent', id, 'memory'] }),
  })
}

/** `POST /v1/agents/{id}/questions` — opens a thread FROM you TO the role
 * (the api client never sends `X-Rocket-Session`, so the daemon treats the
 * caller as the human) and wakes it with a `question` inbox event. */
export function useAskAgent(
  roleId: string | undefined,
): UseMutationResult<AgentQuestion, Error, { body: string; context?: string }> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload) => api.post<AgentQuestion>(`/v1/agents/${roleId}/questions`, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
    },
  })
}

export function useReplyAgentQuestion(): UseMutationResult<
  AgentQuestion,
  Error,
  { id: number; body: string; roleId: string }
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }) => api.post<AgentQuestion>(`/v1/agent-questions/${id}/reply`, { body }),
    onSuccess: (_data, { roleId }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
    },
  })
}

export function useAnswerAgentQuestion(): UseMutationResult<
  AgentQuestion,
  Error,
  { id: number; roleId: string } & ({ body: string; dismiss?: never } | { dismiss: true; body?: never })
> {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body, dismiss }) =>
      api.post<AgentQuestion>(`/v1/agent-questions/${id}/answer`, dismiss ? { dismiss: true } : { body }),
    onSuccess: (_data, { roleId }) => {
      queryClient.invalidateQueries({ queryKey: ['agent', roleId, 'questions'] })
      queryClient.invalidateQueries({ queryKey: ['agent', roleId] })
    },
  })
}
```

- [ ] **Step 7: Wire SSE + invalidation**

In `web/src/lib/sse.ts`, add to `EVENT_TYPES` (after the `task.*` block):

```ts
  // Role lifecycle + Q&A (docs/10-agents.md). `agent.issue_opened` /
  // `agent.issue_comment` come from the GitHub poller's role subscriptions.
  'agent.instance_spawned',
  'agent.run_done',
  'agent.run_timeout',
  'agent.issue_opened',
  'agent.issue_comment',
  'agent.question_asked',
  'agent.question_replied',
  'agent.question_reopened',
  'agent.question_resolved',
```

In `wireInvalidation` (queries.ts), add a branch before the `repo.clone_` one:

```ts
    } else if (event.type.startsWith('agent.')) {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent'] })
      // Instance spawn/exit also moves the session list the runs journal reads.
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
```

- [ ] **Step 8: Run the tests**

Run: `cd web && npx tsc -b && npx vitest run src/lib/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add web/src/lib web/src/mocks
git commit -m "web: agent roles data layer (types, queries, SSE, mocks)"
```

---

### Task 3: Reusable question thread — extract the presentational view

The role Q&A UI must reuse the task thread (plan Task 5: "reuse task threads
components"). `QuestionThread.tsx` today calls task-specific mutation hooks
directly, so extract its markup into a props-driven view and make the existing
component a thin wrapper. No visual change for tasks.

**Files:**
- Create: `web/src/components/QuestionThreadView.tsx`
- Modify: `web/src/components/QuestionThread.tsx`
- Test: `web/src/components/QuestionThreadView.test.tsx`

**Interfaces:**
- Produces:
  ```ts
  export interface ThreadEntry { id: number; author?: string; body: string; created_at: number }
  export interface QuestionThreadViewProps {
    ordinal: number
    body: string
    context?: string
    messages: ThreadEntry[]
    /** '' when nobody is waiting (resolved thread). */
    turnLabel: string
    /** true renders the turn chip in the warning tone ("awaiting you"). */
    turnWarn: boolean
    askerLabel: string
    /** Display name for message authors that are agents (role id / orchestrator name). */
    agentName?: string
    /** Avatar letter for agent-authored entries: 'O' for orchestrators, 'A' for roles. */
    agentInitial?: string
    placeholder?: string
    busy?: boolean
    onClarify: (body: string) => void
    onAnswer: (body: string) => void
    onDismiss: () => void
  }
  export function QuestionThreadView(props: QuestionThreadViewProps): JSX.Element
  ```

- [ ] **Step 1: Write the failing test**

`web/src/components/QuestionThreadView.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { QuestionThreadView } from './QuestionThreadView'

const base = {
  ordinal: 2,
  body: 'Ship it?',
  messages: [{ id: 1, author: 'sre-run-3', body: 'ready', created_at: 1754003000 }],
  turnLabel: 'awaiting you',
  turnWarn: true,
  askerLabel: 'sre asked',
  agentName: 'sre',
  agentInitial: 'A',
  onClarify: vi.fn(),
  onAnswer: vi.fn(),
  onDismiss: vi.fn(),
}

describe('QuestionThreadView', () => {
  it('renders the question, turn chip and agent-authored entries', () => {
    render(<QuestionThreadView {...base} />)
    expect(screen.getByText('Q2')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
    expect(screen.getByText('sre')).toBeInTheDocument()
  })

  it('passes the composed body to onAnswer and clears the textarea', async () => {
    const onAnswer = vi.fn()
    render(<QuestionThreadView {...base} onAnswer={onAnswer} />)
    await userEvent.type(screen.getByLabelText('Reply to Q2'), 'yes, ship')
    await userEvent.click(screen.getByRole('button', { name: /Answer & close/ }))
    expect(onAnswer).toHaveBeenCalledWith('yes, ship')
  })

  it('disables both submit actions while the body is empty', () => {
    render(<QuestionThreadView {...base} />)
    expect(screen.getByRole('button', { name: /Clarify/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Answer & close/ })).toBeDisabled()
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Create the view**

Create `web/src/components/QuestionThreadView.tsx` by moving the JSX out of
`QuestionThread.tsx` verbatim (same class names, same `questionthread.css`),
replacing the hook calls with the props above: `question.ordinal` -> `ordinal`,
`question.messages` -> `messages`, `authorLabel(m.author, orchestratorName)` ->
`m.author ? (agentName ?? m.author) : 'you'`, the avatar letter -> `agentInitial ?? 'O'`,
and `handleClarify`/`handleAnswer` calling `onClarify(body)`/`onAnswer(body)` then
clearing local state. Keep the local `useState` for the body, the `usePasteImage`
hook, the collapsible context and the disabled logic exactly as they are.

- [ ] **Step 4: Rewrite QuestionThread as a wrapper**

`web/src/components/QuestionThread.tsx` keeps its exported name, props and the
`authorLabel` export, and now renders:

```tsx
export function QuestionThread({ taskId, question, orchestratorName }: QuestionThreadProps) {
  const reply = useReplyQuestion()
  const answer = useAnswerQuestion()

  return (
    <QuestionThreadView
      ordinal={question.ordinal}
      body={question.body}
      context={question.context}
      messages={question.messages}
      turnLabel={whoseTurnLabel(question)}
      turnWarn={question.whose_turn === 'user'}
      askerLabel={askerLabel(question, orchestratorName)}
      agentName={orchestratorName}
      agentInitial="O"
      placeholder="Write a reply, ask the orchestrator to rephrase, or give your final answer…"
      busy={reply.isPending || answer.isPending}
      onClarify={(body) => reply.mutate({ id: question.id, body, taskId })}
      onAnswer={(body) => answer.mutate({ id: question.id, body, taskId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, taskId })}
    />
  )
}
```

- [ ] **Step 5: Run the full web suite**

Run: `cd web && npx tsc -b && npm test`
Expected: PASS — including the existing `Questions.test.tsx` and `Task.test.tsx`,
which prove the task threads still behave identically.

- [ ] **Step 6: Commit**

```bash
git add web/src/components
git commit -m "web: extract QuestionThreadView so roles can reuse task threads"
```

---

### Task 4: Agents list screen + route + nav

**Files:**
- Create: `web/src/screens/agents/AgentsScreen.tsx`
- Create: `web/src/screens/agents/AgentCard.tsx`
- Create: `web/src/screens/agents/agents.css`
- Create: `web/src/screens/agents/Agents.test.tsx`
- Modify: `web/src/routes.tsx`
- Modify: `web/src/components/AppShell.tsx`
- Modify: `web/src/screens/kanban/KanbanScreen.tsx`

**Interfaces:**
- Consumes: `useAgents`, `useSessions`, `useSetAgentEnabled`, `useWakeAgent`
  (Task 2); `Badge` from `components/Badge`.
- Produces:
  - `export function AgentsScreen(): JSX.Element` at route `/p/:projectId/agents`
  - `export function liveInstance(sessions: Session[] | undefined, roleId: string): Session | undefined`
    (exported from `AgentCard.tsx`, reused by `AgentScreen` in Task 5)
  - `export interface AgentCardProps { projectId: string; agent: Agent; instance?: Session }`

- [ ] **Step 1: Write the failing test**

`web/src/screens/agents/Agents.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { AgentsScreen } from './AgentsScreen'
import { liveInstance } from './AgentCard'

function renderScreen() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/p/billing/agents']}>
        <Routes>
          <Route path="/p/:projectId/agents" element={<AgentsScreen />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AgentsScreen', () => {
  it('lists the project roles with inbox, dossier and question badges', async () => {
    renderScreen()
    await waitFor(() => expect(screen.getByText('sre')).toBeInTheDocument())
    expect(screen.getByText('triage')).toBeInTheDocument()
    expect(screen.getByText('2 queued')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toBeInTheDocument()
  })

  it('shows the live instance of a role', async () => {
    renderScreen()
    await waitFor(() => expect(screen.getByText('● sre-run-3')).toBeInTheDocument())
  })
})

describe('liveInstance', () => {
  const sessions = [
    { id: 'sre-run-3', kind: 'agent', state: 'running' },
    { id: 'sre-run-2', kind: 'agent', state: 'done' },
    { id: 'triage-run-1', kind: 'agent', state: 'running' },
  ] as never as import('../../lib/types').Session[]

  it('matches only live runs of the exact role', () => {
    expect(liveInstance(sessions, 'sre')?.id).toBe('sre-run-3')
    expect(liveInstance(sessions, 'triag')).toBeUndefined()
  })
})
```

The msw server is started globally by `web/src/vitest.setup.ts`; call
`resetAgents()` in an `afterEach` if the file mutates roles (this one doesn't).

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/agents/Agents.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement AgentCard**

`web/src/screens/agents/AgentCard.tsx`:

```tsx
import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import type { Agent, Session } from '../../lib/types'
import './agents.css'

/**
 * The live run of a role, if any. Role instances are sessions of kind
 * `agent` named `<role>-run-<n>` (docs/10-agents.md) — the id prefix is the
 * only link between a run and its role, so match it exactly (`sre-run-` must
 * not match `sre-x-run-1`) and require a non-terminal state.
 */
export function liveInstance(sessions: Session[] | undefined, roleId: string): Session | undefined {
  return sessions?.find(
    (s) =>
      s.kind === 'agent' &&
      s.id.startsWith(`${roleId}-run-`) &&
      (s.state === 'running' || s.state === 'spawning'),
  )
}

interface Stat {
  tone: BadgeTone
  label: string
}

function agentStats(agent: Agent, instance?: Session): Stat[] {
  const stats: Stat[] = []
  if (!agent.enabled) stats.push({ tone: 'neutral', label: 'disabled' })
  if (instance) stats.push({ tone: 'ok', label: `● ${instance.id}` })
  if (agent.inbox_queued > 0) stats.push({ tone: 'indigo', label: `${agent.inbox_queued} queued` })
  if (agent.items > 0) stats.push({ tone: 'neutral', label: `${agent.items} in dossier` })
  if (agent.awaiting_user > 0) stats.push({ tone: 'warn', label: 'awaiting you' })
  else if (agent.open_questions > 0) stats.push({ tone: 'neutral', label: `${agent.open_questions} open Q` })
  if (stats.length === 0) stats.push({ tone: 'neutral', label: 'idle' })
  return stats
}

export interface AgentCardProps {
  projectId: string
  agent: Agent
  instance?: Session
}

export function AgentCard({ projectId, agent, instance }: AgentCardProps) {
  return (
    <Link to={`/p/${projectId}/agents/${agent.id}`} className="agent-card">
      <div className="agent-card__header">
        <span className={'agent-card__dot ' + (instance ? 'agent-card__dot--live' : 'agent-card__dot--idle')} />
        <span className="agent-card__name">{agent.id}</span>
        <Badge tone="neutral" mono>
          {agent.agent}
        </Badge>
      </div>
      <div className="agent-card__subs">
        {agent.subscriptions.length > 0
          ? agent.subscriptions.map((s) => s.repo).join(', ')
          : 'no GitHub subscriptions'}
        {agent.cron && <span className="agent-card__cron"> · cron {agent.cron}</span>}
      </div>
      <div className="agent-card__stats">
        {agentStats(agent, instance).map((s) => (
          <Badge key={s.label} tone={s.tone}>
            {s.label}
          </Badge>
        ))}
      </div>
      <div className="agent-card__footer">updated {timeAgo(agent.updated_at)}</div>
    </Link>
  )
}
```

Check `components/Badge.tsx` for the exact `BadgeTone` union before using
`'warn'` — if the tone is named differently there (e.g. `'review'`), use the
existing warning-colored tone rather than adding one.

- [ ] **Step 4: Implement AgentsScreen**

```tsx
import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { EmptyState } from '../../components/EmptyState'
import { useAgents, useSessions } from '../../lib/queries'
import { AgentCard, liveInstance } from './AgentCard'
import { AgentFormModal } from './AgentFormModal'
import './agents.css'

export function AgentsScreen() {
  const { projectId } = useParams<{ projectId: string }>()
  const [creating, setCreating] = useState(false)
  const { data: agents } = useAgents(projectId)
  const { data: sessions } = useSessions({ kind: 'agent', project: projectId })

  if (!projectId) return null

  return (
    <div className="agents-screen">
      <div className="agents-screen__header">
        <h1 className="agents-screen__title">Agents</h1>
        <div className="agents-screen__spacer" />
        <button type="button" className="agents-screen__new" onClick={() => setCreating(true)}>
          ＋ role
        </button>
      </div>

      {agents && agents.length === 0 ? (
        <EmptyState
          title="No roles yet"
          description="A role is a standing agent — SRE, issue triage — that wakes on events, keeps a dossier and answers you in threads."
        />
      ) : (
        <div className="agents-screen__grid">
          {(agents ?? []).map((a) => (
            <AgentCard
              key={a.id}
              projectId={projectId}
              agent={a}
              instance={liveInstance(sessions, a.id)}
            />
          ))}
        </div>
      )}

      {creating && <AgentFormModal projectId={projectId} onClose={() => setCreating(false)} />}
    </div>
  )
}
```

`AgentFormModal` lands in Task 5 — until then, stub it in this file's import by
creating `AgentFormModal.tsx` with a placeholder that renders `null`, and finish
it in Task 5. Check `components/EmptyState.tsx` for its real prop names first and
match them.

- [ ] **Step 5: Add agents.css**

Create `web/src/screens/agents/agents.css` mirroring
`web/src/screens/projects/projects.css`: a `.agents-screen` page wrapper with the
same max-width/padding, `.agents-screen__grid` as the same responsive card grid,
and `.agent-card*` rules copied from `.project-card*` (surface, border, radius,
hover) with the dot/stat/footer variants used above. Use only the design tokens
from `styles/tokens.css` — no literal colors.

- [ ] **Step 6: Wire the route and navigation**

`web/src/routes.tsx` — add inside the AppShell children, after the task route:

```tsx
      { path: '/p/:projectId/agents', element: <AgentsScreen /> },
      { path: '/p/:projectId/agents/:roleId', element: <AgentScreen /> },
```

(`AgentScreen` arrives in Task 6; add its route in that task if it doesn't exist
yet, so the tree always compiles.)

`web/src/components/AppShell.tsx` — add a nav link between Kanban and Questions,
using the same `navLinkStyle`, pointing at `projectId ? \`/p/${projectId}/agents\` : '/'`
and labelled `Agents`.

`web/src/screens/kanban/KanbanScreen.tsx` — add an `Agents` link in the board
header next to the existing header actions, styled like the neighbouring links.

- [ ] **Step 7: Run the tests**

Run: `cd web && npx tsc -b && npm test`
Expected: PASS (including `AppShell.test.tsx`).

- [ ] **Step 8: Commit**

```bash
git add web/src/screens/agents web/src/routes.tsx web/src/components/AppShell.tsx web/src/screens/kanban/KanbanScreen.tsx
git commit -m "web: Agents list screen with role cards, route and nav"
```

---

### Task 5: Create/edit role form

**Files:**
- Modify: `web/src/screens/agents/AgentFormModal.tsx` (from placeholder to real)
- Modify: `web/src/screens/agents/agents.css`
- Test: `web/src/screens/agents/AgentForm.test.tsx`

**Interfaces:**
- Consumes: `Modal` from `components/Modal`, `useCreateAgent`, `useUpdateAgent`,
  `AgentFormValues` (Task 2).
- Produces:
  ```ts
  export interface AgentFormModalProps {
    projectId: string
    /** Existing role to edit; omitted for creation. */
    agent?: Agent
    onClose: () => void
    /** Called with the role id after a successful create. */
    onCreated?: (id: string) => void
  }
  export function AgentFormModal(props: AgentFormModalProps): JSX.Element
  /** Parses the textarea subscription syntax `owner/repo[ label=a,b][ mention-only]`. */
  export function parseSubscriptions(text: string): AgentSubscription[]
  export function formatSubscriptions(subs: AgentSubscription[]): string
  ```

- [ ] **Step 1: Write the failing test**

`web/src/screens/agents/AgentForm.test.tsx`:

```tsx
import { describe, expect, it } from 'vitest'
import { formatSubscriptions, parseSubscriptions } from './AgentFormModal'

describe('parseSubscriptions', () => {
  it('parses a repo per line with optional labels and mention-only', () => {
    expect(parseSubscriptions('acme/platform label=bug,ops mention-only\nacme/web')).toEqual([
      { repo: 'acme/platform', labels: ['bug', 'ops'], mention_only: true },
      { repo: 'acme/web', labels: [], mention_only: false },
    ])
  })

  it('ignores blank lines and trims whitespace', () => {
    expect(parseSubscriptions('  \n acme/web \n')).toEqual([
      { repo: 'acme/web', labels: [], mention_only: false },
    ])
  })

  it('round-trips through formatSubscriptions', () => {
    const text = 'acme/platform label=bug mention-only'
    expect(formatSubscriptions(parseSubscriptions(text))).toBe(text)
  })
})
```

Plus a rendering test in the same file:

```tsx
it('creates a role from the form', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const onCreated = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AgentFormModal projectId="billing" onClose={() => {}} onCreated={onCreated} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  await userEvent.type(screen.getByLabelText('Role id'), 'ops')
  await userEvent.type(screen.getByLabelText('Role prompt'), '# Ops')
  await userEvent.click(screen.getByRole('button', { name: 'Create role' }))
  await waitFor(() => expect(onCreated).toHaveBeenCalledWith('ops'))
})

it('rejects an id that is not [a-z0-9-]', async () => {
  // …render as above…
  await userEvent.type(screen.getByLabelText('Role id'), 'Ops Team')
  expect(screen.getByRole('button', { name: 'Create role' })).toBeDisabled()
  expect(screen.getByText('Use lowercase letters, digits and dashes')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/agents/AgentForm.test.tsx`
Expected: FAIL — `parseSubscriptions` is not exported.

- [ ] **Step 3: Implement the parser helpers**

```tsx
const ID_PATTERN = /^[a-z0-9-]+$/

/**
 * Subscriptions are edited as one repo per line — the daemon's structural
 * filters (docs/10-agents.md): `owner/repo [label=a,b] [mention-only]`.
 * Unknown trailing words are ignored rather than rejected so a typo never
 * silently drops the whole repo.
 */
export function parseSubscriptions(text: string): AgentSubscription[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [repo, ...rest] = line.split(/\s+/)
      const labelPart = rest.find((w) => w.startsWith('label='))
      return {
        repo,
        labels: labelPart ? labelPart.slice('label='.length).split(',').filter(Boolean) : [],
        mention_only: rest.includes('mention-only'),
      }
    })
}

export function formatSubscriptions(subs: AgentSubscription[]): string {
  return subs
    .map((s) => {
      const parts = [s.repo]
      if (s.labels && s.labels.length > 0) parts.push(`label=${s.labels.join(',')}`)
      if (s.mention_only) parts.push('mention-only')
      return parts.join(' ')
    })
    .join('\n')
}
```

- [ ] **Step 4: Implement the modal**

Render inside `<Modal>` (check its real props in `components/Modal.tsx`) a form with:
`Role id` text input (disabled when editing, inline error
`Use lowercase letters, digits and dashes` when it fails `ID_PATTERN`),
`Role prompt` textarea (monospace, ~16 rows, markdown — with a
`<Markdown>` preview toggle button `Preview` / `Edit`), `Subscriptions` textarea
(placeholder `acme/platform label=bug mention-only`), `Cron` text input
(placeholder `0 * * * *`, helper text "5 fields; leave empty for no schedule"),
`Agent` select with `claude-code` / `codex`, and a footer with `Cancel` plus
`Create role` / `Save`. Submit calls `useCreateAgent`/`useUpdateAgent` with
`{id, project, prompt, subscriptions: parseSubscriptions(subsText), cron, agent}`;
on success call `onCreated?.(id)` then `onClose()`. Surface a failed mutation as
an inline error line using `error.message` (the api client already unwraps the
daemon envelope) — in particular `agent_exists` for a duplicate id.

- [ ] **Step 5: Run the tests**

Run: `cd web && npx tsc -b && npx vitest run src/screens/agents/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/screens/agents
git commit -m "web: create/edit role form (prompt, subscriptions, cron, agent)"
```

---

### Task 6: Role card shell — header, actions, tabs, terminal

**Files:**
- Create: `web/src/screens/agents/AgentScreen.tsx`
- Create: `web/src/screens/agents/AgentScreen.test.tsx`
- Modify: `web/src/screens/agents/agents.css`
- Modify: `web/src/routes.tsx` (if the route wasn't added in Task 4)

**Interfaces:**
- Consumes: `useAgent`, `useAgentQuestions`, `useSessions`, `useSetAgentEnabled`,
  `useWakeAgent`, `useDeleteAgent` (Task 2); `liveInstance` (Task 4);
  `AgentFormModal` (Task 5); `TermOverlay` from `screens/task/TermOverlay`.
- Produces: `export function AgentScreen(): JSX.Element` at
  `/p/:projectId/agents/:roleId`, rendering the tab components from Tasks 7-8.

- [ ] **Step 1: Write the failing test**

`web/src/screens/agents/AgentScreen.test.tsx`:

```tsx
describe('AgentScreen', () => {
  it('shows the role header with its live instance and enabled state', async () => {
    renderScreen('sre')
    await waitFor(() => expect(screen.getByRole('heading', { name: 'sre' })).toBeInTheDocument())
    expect(screen.getByText('● sre-run-3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
  })

  it('wakes the role with a ping message', async () => {
    renderScreen('sre')
    await waitFor(() => expect(screen.getByLabelText('Ping the role')).toBeInTheDocument())
    await userEvent.type(screen.getByLabelText('Ping the role'), 'status?')
    await userEvent.click(screen.getByRole('button', { name: 'Wake' }))
    await waitFor(() => expect(screen.getByLabelText('Ping the role')).toHaveValue(''))
  })

  it('switches tabs', async () => {
    renderScreen('sre')
    await userEvent.click(await screen.findByRole('tab', { name: /Dossier/ }))
    expect(await screen.findByText('acme/platform#42')).toBeInTheDocument()
  })
})
```

Mock the terminal in this file — xterm needs a real canvas:

```tsx
vi.mock('../task/TermOverlay', () => ({
  TermOverlay: ({ session }: { session: { id: string } }) => <div>term:{session.id}</div>,
}))
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/agents/AgentScreen.test.tsx`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the screen**

Layout, mirroring `TaskScreen.tsx`:

- crumbs: `← <project name> agents` linking to `/p/:projectId/agents`;
- title row: `<h1>{role.id}</h1>`, a `Badge` for `agent.agent`, a live/idle dot
  with the instance id when live, an `enabled`/`disabled` badge;
- meta line: prompt path, cron, subscription repos, `updated {timeAgo}`;
- actions row: `Wake` (with a one-line "Ping the role" input; empty input sends a
  bare `message` wake), `Terminal` (see below), `Enable`/`Disable`, `Edit`
  (opens `AgentFormModal` with `agent`), `Delete` (window.confirm, then
  `useDeleteAgent` + `navigate('/p/:projectId/agents')`);
- tabs (`role="tablist"` + `role="tab"`, same markup as TaskScreen):
  `Questions` (count of open, warn when `awaiting_user > 0`), `Inbox`
  (count of queued), `Dossier` (count of items), `Memory`, `Runs`;
- `{termSession && <TermOverlay session={termSession} onClose={...} />}`.

Terminal button behavior ("wake & open", docs/10-agents.md):

```tsx
  // A role has no process between runs. If an instance is live we attach to
  // it directly; otherwise we wake the role with a `terminal_opened` event
  // and wait for the instance the engine spawns — the daemon debounces wakes
  // (30s by default), so this can take a while and must not look hung.
  const [waking, setWaking] = useState(false)
  const instance = liveInstance(sessions, roleId)

  useEffect(() => {
    if (waking && instance) {
      setTermSession({ id: instance.id, tmux_name: instance.tmux_name })
      setWaking(false)
    }
  }, [waking, instance])

  async function handleTerminal() {
    if (instance) {
      setTermSession({ id: instance.id, tmux_name: instance.tmux_name })
      return
    }
    setWaking(true)
    await wake.mutateAsync({ id: roleId, kind: 'terminal_opened' })
  }
```

While `waking` is true the button shows `waking…` and is disabled; the sessions
query is refetched by the `agent.instance_spawned` SSE invalidation from Task 2,
so add `refetchInterval: waking ? 3000 : false` is NOT needed — instead pass
`useSessions({ kind: 'agent', project: projectId })` and rely on SSE, but also
give the user an explicit `Cancel` next to `waking…` that clears the flag.

- [ ] **Step 4: Run the tests**

Run: `cd web && npx tsc -b && npx vitest run src/screens/agents/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/screens/agents web/src/routes.tsx
git commit -m "web: role card shell — header, wake/enable/delete, tabs, terminal"
```

---

### Task 7: Inbox, Dossier and Runs tabs

**Files:**
- Create: `web/src/screens/agents/InboxTab.tsx`
- Create: `web/src/screens/agents/DossierTab.tsx`
- Create: `web/src/screens/agents/RunsTab.tsx`
- Test: `web/src/screens/agents/AgentTabs.test.tsx`
- Modify: `web/src/screens/agents/agents.css`

**Interfaces:**
- Consumes: `useAgentInbox`, `useAgentItems`, `useSessions` (Task 2).
- Produces:
  - `export function InboxTab({ roleId }: { roleId: string }): JSX.Element`
  - `export function DossierTab({ roleId, projectId }: { roleId: string; projectId: string }): JSX.Element`
  - `export function RunsTab({ roleId, projectId }: { roleId: string; projectId: string }): JSX.Element`
  - `export function summarizeEvent(event: AgentInboxEvent): string` (from InboxTab)

- [ ] **Step 1: Write the failing test**

`web/src/screens/agents/AgentTabs.test.tsx`:

```tsx
describe('summarizeEvent', () => {
  it('renders a message event as its text and sender', () => {
    expect(
      summarizeEvent({ kind: 'message', payload: { text: 'blocked by X', from: 'orch' } } as AgentInboxEvent),
    ).toBe('orch: blocked by X')
  })

  it('renders an issue event as repo#number title', () => {
    expect(
      summarizeEvent({
        kind: 'issue_opened',
        payload: { repo: 'acme/platform', number: 42, title: 'DB migration stuck' },
      } as AgentInboxEvent),
    ).toBe('acme/platform#42 — DB migration stuck')
  })

  it('renders a task_update as the status transition', () => {
    expect(
      summarizeEvent({
        kind: 'task_update',
        payload: { task_id: 12, title: 'Billing v2', from: 'in_progress', to: 'review' },
      } as AgentInboxEvent),
    ).toBe('#12 Billing v2: in_progress → review')
  })

  it('falls back to compact JSON for kinds without a template', () => {
    expect(summarizeEvent({ kind: 'cron', payload: {} } as AgentInboxEvent)).toBe('cron tick')
  })
})

describe('DossierTab', () => {
  it('filters by state and links tasks to the board', async () => {
    renderTab(<DossierTab roleId="sre" projectId="billing" />)
    expect(await screen.findByText('acme/platform#42')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '#12' })).toHaveAttribute(
      'href',
      '/p/billing/tasks/12',
    )
    await userEvent.selectOptions(screen.getByLabelText('State'), 'deferred')
    await waitFor(() => expect(screen.queryByText('acme/platform#42')).not.toBeInTheDocument())
    expect(screen.getByText('acme/platform#43')).toBeInTheDocument()
  })
})

describe('RunsTab', () => {
  it('lists the role runs newest first with their state', async () => {
    renderTab(<RunsTab roleId="sre" projectId="billing" />)
    const rows = await screen.findAllByRole('row')
    expect(rows[1]).toHaveTextContent('sre-run-3')
    expect(rows[1]).toHaveTextContent('running')
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/agents/AgentTabs.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement InboxTab**

```tsx
/**
 * One-line human summary of an inbox event. Payload shapes come from the
 * producers: the wake API (`{text, from}`), the GitHub poller
 * (`{repo, number, title}`), the tasks layer (`{task_id, title, from, to}`)
 * and the Q&A threads (`{question_id, ordinal, entry, text}`).
 */
export function summarizeEvent(event: AgentInboxEvent): string {
  const p = event.payload as Record<string, string | number | undefined>
  switch (event.kind) {
    case 'message':
      return p.from ? `${p.from}: ${p.text ?? ''}` : String(p.text ?? '')
    case 'issue_opened':
    case 'issue_comment':
      return `${p.repo}#${p.number}${p.title ? ` — ${p.title}` : ''}`
    case 'task_update':
      return `#${p.task_id} ${p.title ?? ''}: ${p.from} → ${p.to}`
    case 'question':
      return `Q${p.ordinal} ${p.entry}: ${p.text ?? ''}`
    case 'snooze_expired':
      return `snooze expired: ${p.ref ?? ''}`
    case 'cron':
      return 'cron tick'
    case 'terminal_opened':
      return 'terminal opened'
    default:
      return event.kind
  }
}
```

The tab renders a status filter (`all` / `queued` / `delivered` / `done`) driving
`useAgentInbox(roleId, status)`, then a list of rows: kind badge, summary,
`timeAgo(created_at)` and a status chip (`queued` in the warning tone). Empty
state: "Inbox is empty — nothing woke this role yet."

- [ ] **Step 4: Implement DossierTab**

A `State` `<select>` (`all` plus `new`, `triaged`, `taken`, `deferred`,
`waiting_team`, `in_work`, `resolved`, `closed` — docs/10-agents.md) driving
`useAgentItems(roleId, state)`, and a table with columns: kind, ref, state,
note, task (`<Link to={`/p/${projectId}/tasks/${item.task_id}`}>#{item.task_id}</Link>`
when `task_id > 0`, otherwise `—`), snooze (`snooze_until > 0` -> `until <date>`),
updated. Empty state: "The dossier is empty."

- [ ] **Step 5: Implement RunsTab**

```tsx
export function RunsTab({ roleId, projectId }: { roleId: string; projectId: string }) {
  const { data: sessions } = useSessions({ kind: 'agent', project: projectId, all: true })
  const runs = (sessions ?? [])
    .filter((s) => s.id.startsWith(`${roleId}-run-`))
    .sort((a, b) => b.created_at - a.created_at)
  // …table: run id, state, activity, started (timeAgo(created_at)),
  // ended (timeAgo(updated_at) for terminal states), and a «term ▣» link to
  // /term/<id> for live runs…
}
```

- [ ] **Step 6: Run the tests**

Run: `cd web && npx tsc -b && npx vitest run src/screens/agents/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/screens/agents
git commit -m "web: role inbox, dossier and runs tabs"
```

---

### Task 8: Memory tab and Q&A threads tab

**Files:**
- Create: `web/src/screens/agents/MemoryTab.tsx`
- Create: `web/src/screens/agents/AgentQuestionsTab.tsx`
- Create: `web/src/components/AgentQuestionThread.tsx`
- Test: `web/src/screens/agents/AgentMemoryQuestions.test.tsx`

**Interfaces:**
- Consumes: `useAgentMemory`, `useUpdateAgentMemory`, `useAgentQuestions`,
  `useAskAgent`, `useReplyAgentQuestion`, `useAnswerAgentQuestion` (Task 2);
  `QuestionThreadView` (Task 3); `Markdown` from `components/Markdown`.
- Produces:
  - `export function MemoryTab({ roleId }: { roleId: string }): JSX.Element`
  - `export function AgentQuestionsTab({ roleId }: { roleId: string }): JSX.Element`
  - `export function AgentQuestionThread({ roleId, question }: { roleId: string; question: AgentQuestion }): JSX.Element`

- [ ] **Step 1: Write the failing test**

```tsx
describe('MemoryTab', () => {
  it('renders MEMORY.md and saves an edit', async () => {
    renderTab(<MemoryTab roleId="sre" />)
    expect(await screen.findByText(/how the platform deploys/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const box = screen.getByLabelText('Memory index')
    await userEvent.clear(box)
    await userEvent.type(box, '- [New](new.md)')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByText(/\[New\]\(new\.md\)|New/)).toBeInTheDocument())
  })

  it('lists the fact files with their bodies', async () => {
    renderTab(<MemoryTab roleId="sre" />)
    expect(await screen.findByText('platform.md')).toBeInTheDocument()
    expect(screen.getByText('how it deploys')).toBeInTheDocument()
  })
})

describe('AgentQuestionsTab', () => {
  it('renders the role thread with the awaiting-you chip', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)
    expect(await screen.findByText('Should I close acme/platform#42 now?')).toBeInTheDocument()
    expect(screen.getByText('awaiting you')).toBeInTheDocument()
    expect(screen.getByText('sre asked')).toBeInTheDocument()
  })

  it('opens a new thread to the role', async () => {
    renderTab(<AgentQuestionsTab roleId="sre" />)
    await userEvent.type(await screen.findByLabelText('Ask the role'), 'what is blocking #42?')
    await userEvent.click(screen.getByRole('button', { name: 'Ask' }))
    await waitFor(() => expect(screen.getByLabelText('Ask the role')).toHaveValue(''))
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/agents/AgentMemoryQuestions.test.tsx`
Expected: FAIL — modules not found.

- [ ] **Step 3: Implement MemoryTab**

View mode renders `<Markdown>{memory.index}</Markdown>` plus the memory dir path
in mono, then each fact file as a collapsible card (name, `timeAgo(updated_at)`,
markdown body). Every card — the index included — has an `Edit` button that swaps
in a monospace textarea (`aria-label={`Edit ${name}`}`, the index uses
`aria-label="Memory index"`) with `Save` (`useUpdateAgentMemory({id, file, body})`,
back to view on success) and `Cancel`. Show a mutation error inline; `invalid_file`
should read "Memory files must be plain .md names".

- [ ] **Step 4: Implement AgentQuestionThread**

```tsx
/**
 * A role Q&A thread. Same component as task threads (QuestionThreadView) with
 * the role's mutations wired in: `asked_by === ""` means you opened the thread
 * TO the role, anything else is the role escalating TO you (docs/10-agents.md).
 */
export function AgentQuestionThread({ roleId, question }: AgentQuestionThreadProps) {
  const reply = useReplyAgentQuestion()
  const answer = useAnswerAgentQuestion()

  return (
    <QuestionThreadView
      ordinal={question.ordinal}
      body={question.body}
      context={question.context}
      messages={question.messages}
      turnLabel={
        question.whose_turn === 'user'
          ? 'awaiting you'
          : question.whose_turn === 'role'
            ? `awaiting ${roleId}`
            : ''
      }
      turnWarn={question.whose_turn === 'user'}
      askerLabel={question.asked_by === '' ? `you asked ${roleId}` : `${roleId} asked`}
      agentName={roleId}
      agentInitial="A"
      placeholder={`Write a reply, ask ${roleId} to rephrase, or give your final answer…`}
      busy={reply.isPending || answer.isPending}
      onClarify={(body) => reply.mutate({ id: question.id, body, roleId })}
      onAnswer={(body) => answer.mutate({ id: question.id, body, roleId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, roleId })}
    />
  )
}
```

- [ ] **Step 5: Implement AgentQuestionsTab**

Open threads (sorted by ordinal) rendered as `AgentQuestionThread`, then a
collapsed `Resolved (N)` section with the same component, then an "Ask the role"
composer: textarea `aria-label="Ask the role"` + `Ask` button calling
`useAskAgent(roleId)` and clearing on success, with the hint "This opens a thread
and wakes the role."

- [ ] **Step 6: Run the tests**

Run: `cd web && npx tsc -b && npm test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/screens/agents web/src/components
git commit -m "web: role memory tab and Q&A threads reusing the task thread view"
```

---

### Task 9: Awaiting-answer badges in the projects list + docs

**Files:**
- Modify: `web/src/screens/projects/ProjectCard.tsx`
- Modify: `web/src/screens/projects/ProjectsScreen.tsx` (pass the roles down)
- Test: `web/src/screens/projects/ProjectsScreen.test.tsx`
- Modify: `docs/11-dashboard.md`

**Interfaces:**
- Consumes: `useAgents()` (no project filter — one request for the whole grid).
- Produces: `ProjectCardProps` gains `agents?: Agent[]` (the roles of THIS project,
  already filtered by the screen).

- [ ] **Step 1: Write the failing test**

Append to `web/src/screens/projects/ProjectsScreen.test.tsx`:

```tsx
it('shows a role awaiting-answer badge on the project card', async () => {
  renderScreen()
  expect(await screen.findByText('？1 role awaiting you')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/screens/projects/ProjectsScreen.test.tsx`
Expected: FAIL — text not found.

- [ ] **Step 3: Implement**

In `ProjectsScreen.tsx` call `useAgents()` once and pass
`agents={(allAgents ?? []).filter((a) => a.project === project.id)}` to each card.

In `ProjectCard.tsx` extend `projectStats` with, before the fallback `idle` push:

```ts
  // Roles are the second thing that can be waiting on you (docs/10-agents.md):
  // a thread the role opened and nobody answered yet.
  const rolesAwaiting = (agents ?? []).filter((a) => a.awaiting_user > 0).length
  if (rolesAwaiting > 0) {
    stats.push({ tone: 'warn', label: `？${rolesAwaiting} role${rolesAwaiting > 1 ? 's' : ''} awaiting you` })
  }
```

Use the same tone the design uses for the task "awaiting you" signal; verify the
name in `components/Badge.tsx`.

- [ ] **Step 4: Document the screen**

In `docs/11-dashboard.md`, add a section after "Экран 2: Карточка задачи":

```markdown
## Экран 2b: Agents (роли проекта)

`/p/<project>/agents` — сетка карточек ролей: id, underlying-агент, подписки и
cron, бейджи «● <instance>» (живой инстанс), «N queued» (инбокс), «N in dossier»,
«awaiting you» (роль ждёт вашего ответа в треде), «disabled». Кнопка «＋ role»
открывает форму: id, роль-промпт (markdown с предпросмотром), подписки
(`owner/repo [label=a,b] [mention-only]`, по строке на репозиторий), cron, агент.

Карточка роли (`/p/<project>/agents/<role>`) — шапка с действиями (Wake с полем
пинга, Terminal, Enable/Disable, Edit, Delete) и табы:

- **Questions** — Q&A-треды роли (тот же компонент, что у задач): открытые сверху,
  ответ/уточнение/закрытие, композер «спросить роль» (открывает тред и будит роль);
- **Inbox** — события инбокса с фильтром по статусу (`queued|delivered|done`);
- **Dossier** — досье с фильтром по state, ссылками на задачи канбана и snooze;
- **Memory** — `MEMORY.md` роли (просмотр/правка) + список файлов-фактов (read-only);
- **Runs** — журнал запусков (`<role>-run-<n>`), состояние и терминал живого.

**Терминал.** Живой инстанс — attach в оверлее (тот же `TermOverlay`, что у задач);
если инстанса нет, кнопка «Terminal» будит роль событием `terminal_opened` и
открывает терминал, как только инстанс поднялся (демон дебаунсит пробуждения).

Бейдж «？N roles awaiting you» — на карточке проекта в общем списке.
```

- [ ] **Step 5: Run the full suite**

Run: `cd web && npx tsc -b && npm test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/screens/projects docs/11-dashboard.md
git commit -m "web: role awaiting-answer badges on project cards, dashboard docs"
```

---

### Task 10: Verification and PR

- [ ] **Step 1: Full verification**

```bash
go build ./... && go vet ./... && go test ./internal/...
cd web && npx tsc -b && npm test && npm run build
```

Expected: all green. `npm run build` must not be committed — check
`git status web/dist` is clean (the tracked placeholder is unchanged).

- [ ] **Step 2: End-to-end exercise against a live daemon**

With `rocketd` running, create a role from the UI, wake it with a ping, open the
terminal ("wake & open" path), ask it a question from the Questions tab, edit its
memory index, and confirm the dossier/inbox/runs tabs render real data. Note
anything that fails and fix it via superpowers:systematic-debugging before the PR.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin feature/task-639/web
gh pr create --title "web: dashboard Agents screen (roles, inbox, dossier, memory, threads, terminal)" \
  --body "Task #645 of feature task-639 (persistent agent roles)…"
```
