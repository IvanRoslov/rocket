# Global Agents view (web + mobile) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Agents registered without a project become visible: web gets a global `/agents` route grouped by project (incl. «No project»), mobile's Agents tab lists all agents with an All/per-project/No-project filter.

**Architecture:** The daemon already returns every agent from `GET /v1/agents` when `?project=` is absent and already accepts an empty `project` on `POST /v1/agents` (internal/api/agents.go:181,227). So this is UI-only: a new web screen reusing `AgentCard`/`AgentFormModal`, a project-less detail route `/agents/:roleId` sharing `AgentScreen`, and a local (non-global-state) filter on the mobile tab.

**Tech Stack:** React 19 + react-router + @tanstack/react-query + vitest/msw (web); Expo Router + react-native + jest (mobile).

## Global Constraints

- Keep `/p/:projectId/agents` and `/p/:projectId/agents/:roleId` working unchanged (filtered view).
- Only the **Agents** nav tab in `AppShell` switches to the global route; the Kanban tab keeps its last-project link.
- Copy stays in English, lowercase-kebab agent ids, «No project» is the label for `project === ''`.
- No backend changes. No new deps.
- Verification: `web`: `npx tsc --noEmit`, `npx vitest run`, `npm run build`; `mobile`: `npx tsc --noEmit`, `npx jest`.

---

### Task 1: `AgentCard` and `AgentScreen` work without a project

**Files:**
- Modify: `web/src/screens/agents/AgentCard.tsx` (`AgentCardProps.projectId` → optional)
- Modify: `web/src/screens/agents/AgentScreen.tsx` (drop the `if (!projectId) return null` guard, derive labels from `agent.project`)
- Modify: `web/src/routes.tsx` (add `/agents/:roleId`)
- Test: `web/src/screens/agents/AgentScreen.test.tsx`

**Interfaces:**
- Produces: `AgentCard({ projectId?: string, agent: Agent })` — links to `/p/${projectId}/agents/${id}` when `projectId` is set, else `/agents/${id}`.
- Produces: route `/agents/:roleId` → `<AgentScreen />`; its back link goes to `/agents`, and delete navigates to `/agents`.

- [ ] Step 1: add a test rendering `AgentScreen` at `/agents/sre` (no `:projectId`) asserting the agent id renders and the back link is «← all agents».
- [ ] Step 2: run `npx vitest run src/screens/agents` — expect FAIL (screen returns null).
- [ ] Step 3: make `projectId` optional in both components; back-link/delete target chosen by `projectId` presence; register the route.
- [ ] Step 4: run the agents tests — expect PASS.
- [ ] Step 5: commit.

### Task 2: `AgentFormModal` accepts an empty project

**Files:**
- Modify: `web/src/screens/agents/AgentFormModal.tsx`
- Test: `web/src/screens/agents/AgentForm.test.tsx`

**Interfaces:**
- Produces: `AgentFormModal({ projectId?: string, agent?: Agent, onClose, onCreated? })`. With `projectId` omitted the form shows a **Project** `<select>` listing `useProjects()` plus a `— none —` option (default `''`), and posts `project: ''`.

- [ ] Step 1: test — render without `projectId`, type an id, submit, assert `POST /v1/agents` body has `project: ''`; and a second test asserting the select is absent when `projectId` is given.
- [ ] Step 2: run — FAIL.
- [ ] Step 3: implement the optional prop + select; `project` state initialised from `agent?.project ?? projectId ?? ''`.
- [ ] Step 4: run — PASS.
- [ ] Step 5: commit.

### Task 3: web `GlobalAgentsScreen` at `/agents`

**Files:**
- Create: `web/src/screens/agents/GlobalAgentsScreen.tsx`
- Create: `web/src/screens/agents/GlobalAgents.test.tsx`
- Modify: `web/src/screens/agents/agents.css` (chip row styles)
- Modify: `web/src/routes.tsx`
- Modify: `web/src/mocks/fixtures.ts` (add a project-less agent fixture)

**Interfaces:**
- Consumes: `useAgents()` (no argument → `GET /v1/agents`), `useProjects()`, `AgentCard`, `AgentFormModal`.
- Produces: `GlobalAgentsScreen()` — filter chips `All` + one per project that has agents + `No project`; sections in project-name order with «No project» last; each card links to `/agents/:id`; «＋ New agent» opens the modal without `projectId`.

- [ ] Step 1: tests — (a) both a project agent and a project-less agent are listed under their group headings; (b) clicking the «No project» chip hides the project agents; (c) cards link to `/agents/<id>`.
- [ ] Step 2: run — FAIL (module missing).
- [ ] Step 3: implement screen + route + CSS + fixture.
- [ ] Step 4: run — PASS.
- [ ] Step 5: commit.

### Task 4: AppShell Agents tab → `/agents`

**Files:**
- Modify: `web/src/components/AppShell.tsx`
- Test: `web/src/components/AppShell.test.tsx` (create if absent)

**Interfaces:**
- Produces: the Agents `<Link>` always targets `/agents`; it is `aria-current="page"` on `/agents`, `/agents/:id` and `/p/:id/agents*`. Kanban stays on `navProjectId`.

- [ ] Step 1: test — Agents link href is `/agents` even with a last project set; Kanban href still `/p/<last>`.
- [ ] Step 2: run — FAIL.
- [ ] Step 3: implement (`agentsActive = pathname.startsWith('/agents') || (inProject && pathname.includes('/agents'))`, `kanbanActive = inProject && !pathname.includes('/agents')`).
- [ ] Step 4: run — PASS.
- [ ] Step 5: commit.

### Task 5: mobile Agents tab lists all agents

**Files:**
- Modify: `mobile/app/(tabs)/agents.tsx`
- Test: `mobile/app/agent/agent-screens.test.tsx`

**Interfaces:**
- Produces: `useAgents()` called with no project; local `filter` state (`'all' | '__none__' | <projectId>`); chips `All` + projects that have agents + `No project`; filtering is client-side and does **not** touch `useServers().activeProjectId`.

- [ ] Step 1: tests — a project agent and a project-less agent both render; pressing «No project» leaves only the project-less one; the empty state appears when the filter matches nothing.
- [ ] Step 2: run `npx jest app/agent` — FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run — PASS.
- [ ] Step 5: commit.

### Task 6: docs + full verification

**Files:**
- Modify: `docs/11-dashboard.md`, `docs/10-agents.md` (mention the global view)

- [ ] Step 1: update the docs paragraphs describing the Agents screen.
- [ ] Step 2: run `cd web && npx tsc --noEmit && npx vitest run && npm run build`.
- [ ] Step 3: run `cd mobile && npx tsc --noEmit && npx jest`.
- [ ] Step 4: commit, push, open the PR.

### Task 7: per-card session affordances (owner scope addition)

**Files:**
- Modify: `web/src/screens/agents/AgentCard.tsx`, `agents.css`
- Modify: `mobile/app/(tabs)/agents.tsx`
- Test: `web/src/screens/agents/Agents.test.tsx`, `mobile/app/agent/agent-screens.test.tsx`

**Interfaces:**
- The card is no longer one big `<Link>` (an anchor cannot nest anchors/buttons): `div.agent-card` with the id as the detail link plus an actions row.
- Actions: `▣ term` → `termPagePath(agent.id)`, `💬 chat` → `chatPagePath(agent.id)` (both `target="_blank"`, rendered as disabled spans with `title="session is down"` when `!session_alive`), and `⧉ attach` which always copies `rocket agent attach <id>` and flips to `copied` for 1.5s.
- Mobile mirrors the chat entry (`/chat/<id>`, the app has no term view) and a copy-attach button via `expo-clipboard` if present, otherwise omitted.

- [ ] Step 1: tests for the three affordances + the disabled state.
- [ ] Step 2..5: implement, run, commit.
