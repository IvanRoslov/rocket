# Mobile Agent Roles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring persistent agent roles into the rocket Expo app: a roles list with badges, a role card (inbox, dossier, Q&A threads with reply/answer, wake), read-only prompt and memory, and an open-question badge in the tab bar.

**Architecture:** Pure additive work in `mobile/` on top of the merged `/v1/agents*` API (task #639 core/runtime/github-tasks/qa-threads). Types go into the existing `src/api/types.ts`, data access into `src/api/queries.ts` (TanStack Query, same `[baseUrl, segment, ...]` key shape), presentation helpers into a new pure module `src/lib/agents.ts` (unit-tested), screens into `app/(tabs)/agents.tsx` and `app/agent/[id].tsx` following `app/(tabs)/kanban.tsx` and `app/task/[id].tsx`. **No Go code** — `GET /v1/agents/{id}/memory` is being added by the web worker (#645); the memory tab is coded against that contract and hides itself when the endpoint answers 404.

**Tech Stack:** Expo 57 / expo-router 57 / React Native 0.86, React 19, TanStack Query 5, Jest + jest-expo + @testing-library/react-native.

## Global Constraints

- Expo has changed: consult https://docs.expo.dev/versions/v57.0.0/ before using any Expo API (`mobile/AGENTS.md`).
- `mobile/src/api/types.ts` is a port of `web/src/lib/types.ts` — keep the shapes identical to the API JSON (`internal/api/agents.go`, `internal/api/agent_questions.go`).
- English identifiers, comments and commit messages. Branch `feature/task-639/mobile`, single PR.
- Role ids match `^[a-z0-9-]+$`; roles belong to exactly one project.
- Prompt and memory are **read-only** in mobile v1 (spec v2, "Мобильное приложение").
- No new npm dependencies.
- **Files touched are under `mobile/` and `docs/` only.** `GET /v1/agents/{id}/memory` ships with the web PR (#645); this PR must not add or edit Go code (it would collide in `internal/api`).
- UI copy is English, matching every existing mobile screen (`Projects`, `Kanban`, `System`, `Settings`). The brief's "Russian UI" line conflicts with its own "consistent with the app's copy" requirement; consistency wins, and app-wide localisation is out of scope here.
- Every task ends with `cd mobile && npx jest <files>` green.

---

### Task 1: API types for roles

**Files:**
- Modify: `mobile/src/api/types.ts` (append after the `Question` block, ~line 182)

**Interfaces:**
- Consumes: nothing.
- Produces: `Agent`, `AgentInboxEvent`, `AgentInboxKind`, `AgentItem`, `AgentQuestion`, `AgentMemory` — used by every later task.

- [ ] **Step 1: Add the types**

```ts
// Agent roles — docs/10-agents.md. A role is a durable definition
// (prompt, subscriptions, cron); its runs are ephemeral sessions named
// "<role>-run-<n>".

export interface AgentSubscription {
  repo: string
  labels?: string[]
  mention_only?: boolean
}

export interface Agent {
  id: string
  project: string
  prompt_path: string
  /** Only present on GET /v1/agents/{id}; the list omits it. */
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

export type AgentInboxStatus = 'queued' | 'delivered' | 'done'

export interface AgentInboxEvent {
  id: number
  kind: AgentInboxKind
  payload: Record<string, unknown>
  status: AgentInboxStatus
  created_at: number
  updated_at: number
}

export type AgentItemKind = 'issue' | 'task' | 'ping'

export interface AgentItem {
  id: number
  kind: AgentItemKind
  ref: string
  state: string
  note: string
  task_id: number
  snooze_until: number
  created_at: number
  updated_at: number
}

/** Role Q&A thread — mirrors `Question` with the role in place of the task. */
export interface AgentQuestion {
  id: number
  role_id: string
  ordinal: number
  asked_by: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed'
  whose_turn?: 'user' | 'role' | ''
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

/** Read-only view of the role's file memory (GET /v1/agents/{id}/memory). */
export interface AgentMemory {
  index: string
  files: string[]
}
```

- [ ] **Step 2: Typecheck**

Run: `cd mobile && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add mobile/src/api/types.ts
git commit -m "mobile: API types for agent roles"
```

---

### Task 2: Memory contract (client-side only)

`GET /v1/agents/{id}/memory` does not exist in `main`; the web worker (#645)
adds it. This task pins the contract on our side and makes the screen degrade
to nothing when the daemon predates it — so this PR can land first.

**Files:**
- Modify: `mobile/src/api/types.ts` (the `AgentMemory` interface from Task 1 — verify it against the shape the orchestrator forwards)

**Interfaces:**
- Consumes: nothing.
- Produces: the assumed shape `{"index": "<MEMORY.md>", "files": ["fact.md", ...]}`; a 404 means "this daemon has no memory endpoint".

- [ ] **Step 1: Check whether the contract arrived**

Run: `ls .rocket/inbox/` and read any new message from `task-639-orch`.
If it carries the exact JSON shape from #645, reconcile `AgentMemory` with it
(field names are the API's call, not ours). If it has not arrived, keep the
assumed shape and continue — the fallback in Task 6 makes a mismatch visible
rather than fatal.

- [ ] **Step 2: Make the query tolerate an absent endpoint**

`api.get` rejects on a non-2xx response, so `useAgentMemory` (Task 4) must not
retry a 404 into a spinner. Nothing to write here beyond confirming the hook
sets `retry: false`; Task 6 hides the tab when `memory.isError` is set.

- [ ] **Step 3: No commit of its own**

This task produces no standalone artifact; its outcome is folded into Tasks 4 and 6.

---

### Task 3: Presentation helpers (`src/lib/agents.ts`)

Pure functions so the screens stay declarative and the logic is unit-tested
without rendering.

**Files:**
- Create: `mobile/src/lib/agents.ts`
- Test: `mobile/src/lib/agents.test.ts`

**Interfaces:**
- Consumes: `Agent`, `AgentInboxEvent`, `AgentItem` from Task 1.
- Produces:
  - `awaitingUser(agents: Agent[]): number`
  - `agentBadges(a: Agent): { label: string; fg: string; bg: string }[]`
  - `inboxKindBadge(kind: string): { label: string; fg: string; bg: string }`
  - `itemStateBadge(state: string): { label: string; fg: string; bg: string }`
  - `inboxSummary(e: AgentInboxEvent): string`
  - `subscriptionLabel(s: AgentSubscription): string`

- [ ] **Step 1: Write the failing test**

```ts
import type { Agent, AgentInboxEvent } from '../api/types'
import {
  agentBadges,
  awaitingUser,
  inboxKindBadge,
  inboxSummary,
  itemStateBadge,
  subscriptionLabel,
} from './agents'

const base: Agent = {
  id: 'sre',
  project: 'platform',
  prompt_path: '/tmp/role.md',
  subscriptions: [],
  cron: '',
  agent: 'claude-code',
  enabled: true,
  inbox_queued: 0,
  items: 0,
  open_questions: 0,
  awaiting_user: 0,
  created_at: 1,
  updated_at: 1,
}

describe('awaitingUser', () => {
  it('sums awaiting_user across roles', () => {
    expect(awaitingUser([{ ...base, awaiting_user: 2 }, { ...base, id: 'triage', awaiting_user: 1 }])).toBe(3)
  })

  it('is 0 for no roles', () => {
    expect(awaitingUser([])).toBe(0)
  })
})

describe('agentBadges', () => {
  it('flags a disabled role', () => {
    expect(agentBadges({ ...base, enabled: false }).map((b) => b.label)).toContain('disabled')
  })

  it('shows queued inbox and awaiting-answer counts', () => {
    const labels = agentBadges({ ...base, inbox_queued: 3, awaiting_user: 1, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('3 queued')
    expect(labels).toContain('? 1 awaiting you')
  })

  it('shows open questions without the awaiting flag when the role owes the answer', () => {
    const labels = agentBadges({ ...base, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('2 open Q')
    expect(labels).not.toContain('? 0 awaiting you')
  })

  it('says idle when there is nothing to show', () => {
    expect(agentBadges(base).map((b) => b.label)).toEqual(['idle'])
  })
})

describe('inboxKindBadge', () => {
  it('humanises known kinds', () => {
    expect(inboxKindBadge('issue_opened').label).toBe('issue opened')
  })

  it('passes unknown kinds through', () => {
    expect(inboxKindBadge('whatever').label).toBe('whatever')
  })
})

describe('itemStateBadge', () => {
  it('colours deferred differently from taken', () => {
    expect(itemStateBadge('deferred').bg).not.toBe(itemStateBadge('taken').bg)
  })

  it('labels an empty state as new', () => {
    expect(itemStateBadge('').label).toBe('new')
  })
})

describe('inboxSummary', () => {
  const ev = (payload: Record<string, unknown>, kind = 'message'): AgentInboxEvent =>
    ({ id: 1, kind, payload, status: 'queued', created_at: 1, updated_at: 1 }) as AgentInboxEvent

  it('uses the text field of a message', () => {
    expect(inboxSummary(ev({ text: 'blocked by X', from: 'orch' }))).toBe('blocked by X')
  })

  it('falls back to the issue title', () => {
    expect(inboxSummary(ev({ title: 'db is down' }, 'issue_opened'))).toBe('db is down')
  })

  it('renders the payload compactly when nothing is recognised', () => {
    expect(inboxSummary(ev({ n: 1 }, 'cron'))).toBe('{"n":1}')
  })

  it('is empty for an empty payload', () => {
    expect(inboxSummary(ev({}, 'cron'))).toBe('')
  })
})

describe('subscriptionLabel', () => {
  it('renders repo, labels and mention-only', () => {
    expect(subscriptionLabel({ repo: 'o/r', labels: ['bug'], mention_only: true })).toBe('o/r · bug · @mentions')
  })

  it('renders a bare repo', () => {
    expect(subscriptionLabel({ repo: 'o/r' })).toBe('o/r')
  })
})
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd mobile && npx jest src/lib/agents.test.ts`
Expected: FAIL — `Cannot find module './agents'`.

- [ ] **Step 3: Implement `src/lib/agents.ts`**

```ts
import type { Agent, AgentInboxEvent, AgentSubscription } from '../api/types'
import { colors } from '../theme'

type BadgeProps = { label: string; fg: string; bg: string }

/** Total number of role threads where the next word is the user's. */
export function awaitingUser(agents: Agent[]): number {
  return agents.reduce((n, a) => n + a.awaiting_user, 0)
}

/** Badges for a role card, most urgent first. */
export function agentBadges(a: Agent): BadgeProps[] {
  const out: BadgeProps[] = []
  if (!a.enabled) out.push({ label: 'disabled', fg: colors.slateFg, bg: colors.slateBg })
  if (a.awaiting_user > 0)
    out.push({ label: `? ${a.awaiting_user} awaiting you`, fg: colors.amberDeep, bg: colors.amberBg })
  if (a.open_questions > 0)
    out.push({ label: `${a.open_questions} open Q`, fg: colors.purpleFg, bg: colors.purpleBg })
  if (a.inbox_queued > 0)
    out.push({ label: `${a.inbox_queued} queued`, fg: colors.indigoFg, bg: colors.indigoBg })
  if (a.items > 0) out.push({ label: `${a.items} tracked`, fg: colors.slateFg, bg: colors.slateBg })
  if (out.length === 0) out.push({ label: 'idle', fg: colors.slateFg, bg: colors.slateBg })
  return out
}

const INBOX_KIND_LABEL: Record<string, string> = {
  message: 'message',
  issue_opened: 'issue opened',
  issue_comment: 'issue comment',
  task_update: 'task update',
  snooze_expired: 'snooze expired',
  cron: 'cron',
  question: 'question',
  terminal_opened: 'terminal',
}

const INBOX_KIND_COLOR: Record<string, [string, string]> = {
  message: [colors.indigoFg, colors.indigoBg],
  issue_opened: [colors.purpleFg, colors.purpleBg],
  issue_comment: [colors.purpleFg, colors.purpleBg],
  task_update: [colors.greenFg, colors.greenBg],
  question: [colors.amberDeep, colors.amberBg],
}

export function inboxKindBadge(kind: string): BadgeProps {
  const [fg, bg] = INBOX_KIND_COLOR[kind] ?? [colors.slateFg, colors.slateBg]
  return { label: INBOX_KIND_LABEL[kind] ?? kind, fg, bg }
}

const ITEM_STATE_COLOR: Record<string, [string, string]> = {
  taken: [colors.indigoFg, colors.indigoBg],
  in_work: [colors.indigoFg, colors.indigoBg],
  deferred: [colors.amberDeep, colors.amberBg],
  waiting_team: [colors.amberDeep, colors.amberBg],
  resolved: [colors.greenFg, colors.greenBg],
  closed: [colors.slateFg, colors.slateBg],
}

export function itemStateBadge(state: string): BadgeProps {
  const [fg, bg] = ITEM_STATE_COLOR[state] ?? [colors.slateFg, colors.slateBg]
  return { label: state || 'new', fg, bg }
}

/** One-line preview of an inbox event: the human-readable field, or the raw payload. */
export function inboxSummary(e: AgentInboxEvent): string {
  const p = (e.payload ?? {}) as Record<string, unknown>
  for (const key of ['text', 'title', 'body', 'comment', 'status']) {
    const v = p[key]
    if (typeof v === 'string' && v.trim()) return v.trim()
  }
  if (Object.keys(p).length === 0) return ''
  return JSON.stringify(p)
}

export function subscriptionLabel(s: AgentSubscription): string {
  const parts = [s.repo]
  if (s.labels?.length) parts.push(s.labels.join(', '))
  if (s.mention_only) parts.push('@mentions')
  return parts.join(' · ')
}
```

- [ ] **Step 4: Run the test**

Run: `cd mobile && npx jest src/lib/agents.test.ts`
Expected: PASS. If `colors.amberDeep`/`colors.purpleFg` etc. are absent from
`src/theme.ts`, use the nearest existing key rather than adding colors.

- [ ] **Step 5: Commit**

```bash
git add mobile/src/lib/agents.ts mobile/src/lib/agents.test.ts
git commit -m "mobile: presentation helpers for agent roles"
```

---

### Task 4: Query and mutation hooks

**Files:**
- Modify: `mobile/src/api/queries.ts` (queries after `useTaskQuestions`, mutations at the end)
- Modify: `mobile/src/api/events.ts` (`parseEventType`)
- Test: `mobile/src/api/agents.test.tsx`
- Test: `mobile/src/api/events.test.ts` (append one case)

**Interfaces:**
- Consumes: Task 1 types, Task 2 endpoint, `api` from `./client`, `usePoll`/`useBaseUrl` already in `queries.ts`.
- Produces:
  - `useAgents(project?: string)` → `Agent[]`
  - `useAgent(id: string)` → `Agent` (includes `prompt`)
  - `useAgentInbox(id: string, enabled: boolean)` → `AgentInboxEvent[]`
  - `useAgentItems(id: string, enabled: boolean)` → `AgentItem[]`
  - `useAgentMemory(id: string, enabled: boolean)` → `AgentMemory`
  - `useAgentQuestions(id: string)` → `AgentQuestion[]`
  - `useWakeAgent()` → mutate `{ id, text }`
  - `useSetAgentEnabled()` → mutate `{ id, enabled }`
  - `useCreateAgentQuestion()` → mutate `{ roleId, body, context? }`
  - `useAgentQuestionReply()` / `useAgentQuestionAnswer()` → mutate `{ id, body }`
  - `useAgentQuestionDismiss()` → mutate `id`

- [ ] **Step 1: Write the failing tests**

`mobile/src/api/agents.test.tsx` — copy the `wrapper`/`setup`/`lastCall`
helpers from `mutations.test.tsx` verbatim (that file is the established
pattern; duplicating the 30-line harness is preferred over refactoring a
working test file), then:

```ts
describe('agent hooks', () => {
  afterEach(() => jest.restoreAllMocks())

  it('lists roles of a project', async () => {
    const r = await setup(() => useAgents('platform'))
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(lastCall().url).toBe(`${BASE}/v1/agents?project=platform`)
  })

  it('lists all roles when no project is given', async () => {
    const r = await setup(() => useAgents())
    await waitFor(() => expect(lastCall().url).toBe(`${BASE}/v1/agents`))
  })

  it('wakes a role with text', async () => {
    const r = await setup(useWakeAgent)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', text: 'ping' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agents/sre/wake`)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({ kind: 'message', text: 'ping' })
  })

  it('disables a role', async () => {
    const r = await setup(useSetAgentEnabled)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', enabled: false })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agents/sre/disable`)
  })

  it('enables a role', async () => {
    const r = await setup(useSetAgentEnabled)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 'sre', enabled: true })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agents/sre/enable`)
  })

  it('opens a thread on a role', async () => {
    const r = await setup(useCreateAgentQuestion)
    await act(async () => {
      await r.current.hook.mutateAsync({ roleId: 'sre', body: 'status?' })
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agents/sre/questions`)
    expect(JSON.parse(init.body as string)).toEqual({ body: 'status?', context: undefined })
  })

  it('replies in a role thread', async () => {
    const r = await setup(useAgentQuestionReply)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 5, body: 'more' })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agent-questions/5/reply`)
  })

  it('answers a role thread', async () => {
    const r = await setup(useAgentQuestionAnswer)
    await act(async () => {
      await r.current.hook.mutateAsync({ id: 5, body: 'yes' })
    })
    expect(lastCall().url).toBe(`${BASE}/v1/agent-questions/5/answer`)
  })

  it('dismisses a role thread', async () => {
    const r = await setup(useAgentQuestionDismiss)
    await act(async () => {
      await r.current.hook.mutateAsync(5)
    })
    const { url, init } = lastCall()
    expect(url).toBe(`${BASE}/v1/agent-questions/5/answer`)
    expect(JSON.parse(init.body as string)).toEqual({ dismiss: true })
  })
})
```

Append to `mobile/src/api/events.test.ts`:

```ts
  it('agent events invalidate the roles queries', () => {
    expect(parseEventType('agent.question_asked')).toEqual(['agents', 'agent'])
    expect(parseEventType('agent.instance_spawned')).toEqual(['agents', 'agent'])
  })
```

- [ ] **Step 2: Run them, verify they fail**

Run: `cd mobile && npx jest src/api/agents.test.tsx src/api/events.test.ts`
Expected: FAIL — `useAgents` is not exported; `parseEventType('agent.…')` returns `[]`.

- [ ] **Step 3: Implement the hooks**

In `mobile/src/api/queries.ts`, extend the type import with `Agent`,
`AgentInboxEvent`, `AgentItem`, `AgentMemory`, `AgentQuestion`, then add the
queries after `useTaskQuestions`:

```ts
// --- Agent roles (docs/10-agents.md) ---------------------------------------

export function useAgents(project?: string) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  const qs = project ? `?project=${encodeURIComponent(project)}` : ''
  return useQuery({
    queryKey: [baseUrl, 'agents', project ?? 'all'],
    queryFn: () => api.get<Agent[]>(baseUrl, `/v1/agents${qs}`),
    refetchInterval,
  })
}

export function useAgent(id: string) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'agent', id],
    queryFn: () => api.get<Agent>(baseUrl, `/v1/agents/${id}`),
    refetchInterval,
  })
}

export function useAgentInbox(id: string, enabled: boolean) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'agent', id, 'inbox'],
    queryFn: () => api.get<AgentInboxEvent[]>(baseUrl, `/v1/agents/${id}/inbox`),
    enabled,
    refetchInterval,
  })
}

export function useAgentItems(id: string, enabled: boolean) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(5000)
  return useQuery({
    queryKey: [baseUrl, 'agent', id, 'items'],
    queryFn: () => api.get<AgentItem[]>(baseUrl, `/v1/agents/${id}/items`),
    enabled,
    refetchInterval,
  })
}

// Memory is served by an endpoint the web PR (#645) adds. On a daemon that
// predates it the request 404s — no retries, and the screen hides the tab.
export function useAgentMemory(id: string, enabled: boolean) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(15000, 60000)
  return useQuery({
    queryKey: [baseUrl, 'agent', id, 'memory'],
    queryFn: () => api.get<AgentMemory>(baseUrl, `/v1/agents/${id}/memory`),
    enabled,
    retry: false,
    refetchInterval,
  })
}

export function useAgentQuestions(id: string) {
  const baseUrl = useBaseUrl()
  const refetchInterval = usePoll(3000)
  return useQuery({
    queryKey: [baseUrl, 'agent', id, 'questions'],
    queryFn: async () =>
      (await api.get<{ questions: AgentQuestion[] }>(baseUrl, `/v1/agents/${id}/questions`)).questions ?? [],
    refetchInterval,
  })
}
```

Mutations at the end of the file:

```ts
/** Pings a role: enqueues a `message` inbox event, which wakes an instance. */
export function useWakeAgent() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: string; text: string }) =>
      api.post(baseUrl, `/v1/agents/${p.id}/wake`, { kind: 'message', text: p.text }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'agents'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] })
    },
  })
}

export function useSetAgentEnabled() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: string; enabled: boolean }) =>
      api.post<Agent>(baseUrl, `/v1/agents/${p.id}/${p.enabled ? 'enable' : 'disable'}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [baseUrl, 'agents'] })
      qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] })
    },
  })
}

/** Opens a user-initiated thread on a role (asked_by=""), waking it. */
export function useCreateAgentQuestion() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { roleId: string; body: string; context?: string }) =>
      api.post<AgentQuestion>(baseUrl, `/v1/agents/${p.roleId}/questions`, {
        body: p.body,
        context: p.context,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] }),
  })
}

export function useAgentQuestionReply() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; body: string }) =>
      api.post(baseUrl, `/v1/agent-questions/${p.id}/reply`, { body: p.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] }),
  })
}

export function useAgentQuestionAnswer() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (p: { id: number; body: string }) =>
      api.post(baseUrl, `/v1/agent-questions/${p.id}/answer`, { body: p.body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] }),
  })
}

export function useAgentQuestionDismiss() {
  const baseUrl = useBaseUrl()
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.post(baseUrl, `/v1/agent-questions/${id}/answer`, { dismiss: true }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [baseUrl, 'agent'] }),
  })
}
```

In `mobile/src/api/events.ts`, add to the `switch` in `parseEventType`:

```ts
    case 'agent':
      return ['agents', 'agent']
```

- [ ] **Step 4: Run the tests**

Run: `cd mobile && npx jest src/api && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mobile/src/api/queries.ts mobile/src/api/events.ts mobile/src/api/agents.test.tsx mobile/src/api/events.test.ts
git commit -m "mobile: query and mutation hooks for agent roles"
```

---

### Task 5: Agents tab — roles list with badges

**Files:**
- Create: `mobile/app/(tabs)/agents.tsx`
- Modify: `mobile/app/(tabs)/_layout.tsx` (new `Tabs.Screen`, icon, badge)

**Interfaces:**
- Consumes: `useAgents`, `awaitingUser`, `agentBadges`, `subscriptionLabel`, `useServers().activeProjectId`.
- Produces: route `/(tabs)/agents`; each card navigates to `/agent/<id>`.

- [ ] **Step 1: Write the screen**

`mobile/app/(tabs)/agents.tsx` — same skeleton as `app/(tabs)/index.tsx`
(SafeAreaView + header + ConnectionBanner + ScrollView with RefreshControl):

```tsx
import { router } from 'expo-router'
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native'
import { SafeAreaView } from 'react-native-safe-area-context'
import { useAgents, useProjects, useSessions } from '../../src/api/queries'
import type { Agent } from '../../src/api/types'
import { ConnectionBanner } from '../../src/components/ConnectionBanner'
import { Badge, Card, ChipTabs, Dot, EmptyState, MonoText } from '../../src/components/ui'
import { agentBadges, subscriptionLabel } from '../../src/lib/agents'
import { ago } from '../../src/lib/format'
import { useServers } from '../../src/servers/ServerContext'
import { colors, radius } from '../../src/theme'

/** A role runs as sessions named "<role>-run-<n>" — one at a time, at most. */
function liveRun(sessions: { id: string; kind: string; state: string }[] | undefined, id: string) {
  return sessions?.find(
    (s) => s.kind === 'agent' && s.id.startsWith(`${id}-run-`) && (s.state === 'running' || s.state === 'spawning'),
  )
}

function AgentCard({ agent, live }: { agent: Agent; live: boolean }) {
  return (
    <Pressable onPress={() => router.navigate(`/agent/${agent.id}`)}>
      <Card>
        <View style={styles.head}>
          <Dot color={live ? colors.green : agent.enabled ? colors.slate : colors.textFaint} size={9} />
          <MonoText style={styles.name}>{agent.id}</MonoText>
          <View style={styles.idPill}>
            <MonoText style={{ fontSize: 11, color: colors.textFaint }}>{agent.project}</MonoText>
          </View>
        </View>
        {agent.subscriptions.length > 0 ? (
          <MonoText style={{ fontSize: 12.5, marginBottom: 10 }} numberOfLines={2}>
            ⌘ {agent.subscriptions.map(subscriptionLabel).join('  ·  ')}
          </MonoText>
        ) : null}
        <View style={styles.statsRow}>
          {live ? <Badge label="● live" fg={colors.greenFg} bg={colors.greenBg} /> : null}
          {agentBadges(agent).map((b) => (
            <Badge key={b.label} {...b} />
          ))}
        </View>
        <View style={styles.footer}>
          {agent.cron ? <MonoText style={{ fontSize: 11.5 }}>⏱ {agent.cron}</MonoText> : null}
          <View style={{ flex: 1 }} />
          <Text style={{ fontSize: 11.5, color: colors.textFaint }}>{ago(agent.updated_at)}</Text>
        </View>
      </Card>
    </Pressable>
  )
}

export default function AgentsScreen() {
  const { activeProjectId, setActiveProjectId } = useServers()
  const projects = useProjects()
  const agents = useAgents(activeProjectId ?? undefined)
  const sessions = useSessions(activeProjectId ?? undefined)

  const chips = [
    { key: 'all', label: 'All projects' },
    ...(projects.data ?? []).map((p) => ({ key: p.id, label: p.name })),
  ]

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }} edges={['top']}>
      <ConnectionBanner />
      <ScrollView
        contentContainerStyle={{ padding: 16, paddingBottom: 24 }}
        refreshControl={<RefreshControl refreshing={false} onRefresh={() => agents.refetch()} />}
      >
        <Text style={styles.h1}>Agents</Text>
        <Text style={styles.lede}>
          Roles are permanent; their runs are not. A role wakes on events in its inbox, works through them and exits.
        </Text>
        <View style={{ marginBottom: 14 }}>
          <ChipTabs
            chips={chips}
            active={activeProjectId ?? 'all'}
            onChange={(k) => setActiveProjectId(k === 'all' ? null : k)}
          />
        </View>
        <View style={{ gap: 12 }}>
          {(agents.data ?? []).map((a) => (
            <AgentCard key={a.id} agent={a} live={!!liveRun(sessions.data, a.id)} />
          ))}
          {agents.isSuccess && agents.data.length === 0 ? (
            <EmptyState text="No roles yet. Create one with `rocket agent add` or in the dashboard." />
          ) : null}
        </View>
      </ScrollView>
    </SafeAreaView>
  )
}

const styles = StyleSheet.create({
  h1: { fontSize: 24, fontWeight: '700', color: colors.text, letterSpacing: -0.4, marginBottom: 3 },
  lede: { fontSize: 13.5, lineHeight: 20, color: colors.textDim, marginBottom: 18 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 9, marginBottom: 11 },
  name: { fontSize: 16, fontWeight: '700', color: colors.text, flex: 1 },
  idPill: { backgroundColor: colors.page, paddingHorizontal: 8, paddingVertical: 3, borderRadius: radius.sm },
  statsRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, marginBottom: 12 },
  footer: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    borderTopWidth: 1,
    borderTopColor: colors.borderSoft,
    paddingTop: 11,
  },
})
```

- [ ] **Step 2: Register the tab with an open-question badge**

In `mobile/app/(tabs)/_layout.tsx`, add an icon component beside the existing
ones and a screen between `kanban` and `system`. The badge count comes from
the roles list, so the layout subscribes to it:

```tsx
function AgentsIcon({ color }: { color: import('react-native').ColorValue }) {
  return (
    <View style={{ width: 20, height: 20, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 14, height: 11, borderRadius: 4, borderWidth: 1.6, borderColor: color }} />
      <View style={{ position: 'absolute', top: 0, width: 2, height: 4, backgroundColor: color }} />
    </View>
  )
}
```

```tsx
export default function TabsLayout() {
  const agents = useAgents()
  const awaiting = awaitingUser(agents.data ?? [])
  // ...
      <Tabs.Screen
        name="agents"
        options={{
          title: 'Agents',
          tabBarIcon: ({ color }) => <AgentsIcon color={color} />,
          ...(awaiting > 0 ? { tabBarBadge: awaiting } : {}),
        }}
      />
```

with imports `import { useAgents } from '../../src/api/queries'` and
`import { awaitingUser } from '../../src/lib/agents'`.

- [ ] **Step 3: Typecheck and run the suite**

Run: `cd mobile && npx tsc --noEmit && npx jest`
Expected: PASS (no new tests here; the screen is covered by the manual pass in Task 7).

- [ ] **Step 4: Commit**

```bash
git add mobile/app/\(tabs\)/agents.tsx mobile/app/\(tabs\)/_layout.tsx
git commit -m "mobile: Agents tab with roles list and awaiting badge"
```

---

### Task 6: Role card screen

**Files:**
- Create: `mobile/app/agent/[id].tsx`

**Interfaces:**
- Consumes: every hook from Task 4 and every helper from Task 3.
- Produces: route `/agent/<id>` with tabs `Questions | Inbox | Dossier | Prompt | Memory`, a wake composer, and enable/disable in the header menu.

- [ ] **Step 1: Write the screen**

Model it on `app/task/[id].tsx`: header (`BackButton`, role id, live dot,
`⋯` → `ActionSheet`), `ChipTabs`, `ScrollView`, `KeyboardAvoidingView`, and a
`RoleQuestionCard` cloned from that file's `QuestionCard` with these
differences:

- `mine = !q.asked_by` (thread the user opened; the role owes the answer) —
  same flip as tasks;
- the counterpart label is the role id instead of `orch`, and
  `whose_turn === 'user'` renders "awaiting you" while anything else renders
  `waiting for <role>`;
- reply/answer/dismiss go through `useAgentQuestionReply` /
  `useAgentQuestionAnswer` / `useAgentQuestionDismiss`;
- the "＋ Ask the role" button opens a `BottomSheet` composer backed by
  `useCreateAgentQuestion` (body + optional context), same as `AskQuestionSheet`.

Tab bodies:

- **Questions** — open threads as cards, then a `RESOLVED` list of one-liners
  (`Badge Q<ordinal>` + truncated body + `ago(resolved_at)`), exactly like the
  task screen.
- **Inbox** — `useAgentInbox(id, tab === 'inbox')`, newest first, each row a
  `Card` with `Badge {...inboxKindBadge(e.kind)}`, a status badge
  (`queued` amber / `delivered` indigo / `done` slate), `ago(e.created_at)` and
  `inboxSummary(e)` as the body (`numberOfLines={3}`). `EmptyState` "Inbox is
  empty." when the query succeeded with no rows.
- **Dossier** — `useAgentItems(id, tab === 'dossier')` with a state filter row
  (`ChipTabs` over `all` + the states present in the data). Each row:
  `Badge {...itemStateBadge(it.state)}`, `MonoText` ref, `note`, and — when
  `it.task_id` — a `Pressable` "→ task #<id>" navigating to
  `/task/${it.task_id}`; when `it.snooze_until` — `⏰ until <ago>`.
- **Prompt** — `<Markdown>{agent.prompt ?? ''}</Markdown>` under a
  `Badge label="read-only"`, plus subscriptions and cron rendered with
  `subscriptionLabel`.
- **Memory** — `useAgentMemory(id, tab === 'memory')`: `<Markdown>` of
  `memory.index` and a list of `memory.files` as `MonoText` rows, under the same
  `read-only` badge. The endpoint ships with the web PR, so the tab must
  disappear rather than error on a daemon without it: probe it up-front with
  `const memory = useAgentMemory(id, true)` and drop the chip when
  `memory.isError` — `...(memory.isError ? [] : [{ key: 'memory', label: 'Memory' }])`.
  If the active tab was `memory` when the error lands, fall back to `prompt`.

Bottom bar: a pinned composer (same placement as the task screen's sessions
bar) with a `TextInput` "Ping the role…" and a `PrimaryButton` "Wake" wired to
`useWakeAgent`; disabled while `isPending` or the text is blank; clears on
success, `toast.show((e as Error).message)` on error.

Header `ActionSheet` actions:

```tsx
actions={[
  {
    label: agent.enabled ? 'Disable role' : 'Enable role',
    destructive: agent.enabled,
    onPress: () => setEnabled.mutate({ id: agent.id, enabled: !agent.enabled }, { onError: onErr }),
  },
  {
    label: 'Wake now',
    onPress: () => wake.mutate({ id: agent.id, text: 'ping from mobile' }, { onError: onErr }),
  },
]}
```

Loading/未found guard, copied from the task screen:

```tsx
if (!agent) {
  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: colors.page }}>
      <EmptyState text={detail.isError ? 'Failed to load role.' : 'Loading…'} />
    </SafeAreaView>
  )
}
```

Chips definition (open-question count and warn flag mirror the task screen):

```tsx
const chips = [
  { key: 'questions', label: 'Questions', ...(open.length > 0 ? { count: open.length } : {}), warn: awaiting.length > 0 },
  { key: 'inbox', label: 'Inbox', ...(agent.inbox_queued > 0 ? { count: agent.inbox_queued } : {}) },
  { key: 'dossier', label: 'Dossier', ...(agent.items > 0 ? { count: agent.items } : {}) },
  { key: 'prompt', label: 'Prompt' },
  { key: 'memory', label: 'Memory' },
]
```

- [ ] **Step 2: Typecheck and run the suite**

Run: `cd mobile && npx tsc --noEmit && npx jest`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add mobile/app/agent/\[id\].tsx
git commit -m "mobile: role card with inbox, dossier, threads, prompt and memory"
```

---

### Task 7: End-to-end verification and docs

**Files:**
- Modify: `docs/10-agents.md` (a "Мобильное приложение" section)
- Modify: `docs/11-dashboard.md` only if it already documents mobile screens — otherwise leave it.

- [ ] **Step 1: Run the whole suite**

Run: `cd mobile && npx jest && npx tsc --noEmit`
Expected: all green. Record the output. (No Go changes in this PR — `git diff main --stat` must list only `mobile/` and `docs/`.)

- [ ] **Step 2: Exercise it against a live daemon**

```bash
rocket agent add mobiletest --project rocket --prompt-file /dev/null || true
curl -s localhost:4477/v1/agents | head
curl -s -XPOST localhost:4477/v1/agents/mobiletest/wake -d '{"text":"hello"}'
curl -s localhost:4477/v1/agents/mobiletest/inbox
curl -s -XPOST localhost:4477/v1/agents/mobiletest/questions -d '{"body":"status?"}'
curl -s -i localhost:4477/v1/agents/mobiletest/memory   # 404 until #645 merges — the tab must hide
```

Confirm every shape matches the TypeScript types added in Task 1 — field
names, and that `subscriptions` is `[]` rather than `null`. Fix the types if
they disagree; the API is the source of truth.

- [ ] **Step 3: Document the mobile surface**

Add to `docs/10-agents.md`:

```markdown
## Мобильное приложение

Таб **Agents**: список ролей проекта (живость инстанса, очередь инбокса,
открытые вопросы, бейдж «ждёт ответа» на самом табе), карточка роли —
вкладки Questions / Inbox / Dossier / Prompt / Memory, ответ в Q&A-тредах
и кнопка wake. Промпт и память — только чтение; вкладка Memory показывается,
только если демон отвечает на `GET /v1/agents/{id}/memory`.
```

- [ ] **Step 4: Commit and open the PR**

```bash
git add docs/10-agents.md
git commit -m "docs: mobile agent roles surface"
git push -u origin feature/task-639/mobile
gh pr create --title "mobile: agent roles in the app" --body "..."
```
