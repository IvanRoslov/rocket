# Web Agents v4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify the web dashboard's Agents screens to spec v4 — an agent is a registration plus a tmux session, with an inbox of messages, Q&A threads, a terminal and a Start button.

**Architecture:** The web layer is a thin mirror of `internal/api/agents.go`. Types and react-query hooks are rewritten to the v4 shapes, the Dossier/Memory/Runs tabs and the prompt/subscriptions/cron form fields are deleted, and liveness comes from the server's `session_alive` field instead of matching `<role>-run-<n>` session ids.

**Tech Stack:** React 18 + TypeScript, react-router, @tanstack/react-query, MSW + vitest + @testing-library/react.

## Global Constraints

- Agent model: `{id, description, project, dir, command, enabled, session_alive, unread, open_questions, awaiting_user, created_at, updated_at}`.
- Inbox message: `{id, from, body, status: 'unread' | 'read', created_at, read_at?}`.
- Endpoints in use: `GET/POST /v1/agents`, `GET/PATCH/DELETE /v1/agents/{id}`, `POST /v1/agents/{id}/enable|disable`, `POST /v1/agents/{id}/messages`, `GET /v1/agents/{id}/inbox[?status=]`, `POST /v1/agents/{id}/inbox/next`, `GET /v1/agents/{id}/inbox/{msg}`, `POST /v1/agents/{id}/start`, `POST /v1/agents/{id}/stop`, plus the unchanged `/v1/agents/{id}/questions` + `/v1/agent-questions/*`.
- GONE — no code may reference them: `/wake`, `/memory`, `/items`, agent `prompt`/`prompt_path`/`subscriptions`/`cron`/`agent` fields, `inbox_queued`, `items`.
- The agent's tmux session is named after the agent, so the terminal attaches to session id `<agent.id>` with `tmux_name === agent.id`.
- Verification for every task: `npx tsc -b` and `npx vitest run` from `web/`; the final task also runs `npm run build`.

---

### Task 1: v4 types, hooks and MSW fixtures

**Files:**
- Modify: `web/src/lib/types.ts` (Agent roles block, ~line 425-531)
- Modify: `web/src/lib/queries.ts` (Agent roles block, ~line 617-855)
- Modify: `web/src/mocks/fixtures.ts` (agents, agentInbox, drop agentItems/agentMemory/agentPrompt/agentRuns)
- Modify: `web/src/mocks/handlers.ts` (agent routes)
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Produces: `interface Agent {id, description, project, dir, command, enabled, session_alive, unread, open_questions, awaiting_user, created_at, updated_at}`; `interface AgentInboxMessage {id, from, body, status: 'unread'|'read', created_at, read_at?}`; hooks `useAgents(projectId?)`, `useAgent(id?)`, `useAgentInbox(id?, status?)`, `useAgentQuestions(id?)`, `useCreateAgent()`, `useUpdateAgent()`, `useDeleteAgent()`, `useSetAgentEnabled()`, `useSendAgentMessage()`, `useStartAgent()`, `useStopAgent()`; `interface AgentFormValues {id, project, description, dir, command}`.
- Removed: `AgentSubscription`, `AgentInboxKind`, `AgentInboxEvent`, `AgentItem`, `AgentMemoryFile`, `AgentMemory`, `useAgentItems`, `useAgentMemory`, `useUpdateAgentMemory`, `useWakeAgent`.

- [ ] **Step 1: Write the failing tests** in `queries.test.tsx` — `useAgentInbox` returns messages with `from`/`body`/`status`; `useSendAgentMessage` POSTs to `/v1/agents/{id}/messages` and returns `{status:'inbox'|'queued', live}`; `useStartAgent` POSTs `/v1/agents/{id}/start`; `useStopAgent` POSTs `/v1/agents/{id}/stop`.
- [ ] **Step 2: Run `npx vitest run src/lib/queries.test.tsx`** — expect failures (hooks not exported).
- [ ] **Step 3: Rewrite the types block, the hooks block, the fixtures and the MSW handlers** to the v4 shapes above; the handlers keep a mutable `agentsState` and an `agentInboxState` so `messages`/`start`/`stop` flip `unread`/`session_alive`.
- [ ] **Step 4: Run `npx vitest run src/lib/queries.test.tsx`** — expect PASS. Other suites still fail (screens not migrated yet); that is Task 2/3.
- [ ] **Step 5: Commit** `web: agents API layer v4 (#639)`.

### Task 2: Agents list — card and screen

**Files:**
- Modify: `web/src/screens/agents/AgentCard.tsx`, `web/src/screens/agents/AgentsScreen.tsx`, `web/src/screens/agents/AgentFormModal.tsx`
- Modify: `web/src/screens/projects/ProjectCard.tsx` (only if it touched removed fields — it uses `awaiting_user`, which survives)
- Test: `web/src/screens/agents/Agents.test.tsx`, `web/src/screens/agents/AgentForm.test.tsx`

**Interfaces:**
- Consumes: Task 1's `Agent`, `useAgents`, `useCreateAgent`, `useUpdateAgent`.
- Produces: `agentStats(agent: Agent): Stat[]`; `AgentCardProps {projectId, agent}` (no `instance` prop, no `liveInstance` helper).

- [ ] **Step 1: Write the failing tests** — the list renders id + description, a live dot for `session_alive`, badges `disabled` / `N unread` / `awaiting you`; the form has exactly the fields id, description, dir, command and no prompt/subscriptions/cron/agent controls.
- [ ] **Step 2: Run `npx vitest run src/screens/agents`** — expect FAIL.
- [ ] **Step 3: Rewrite the three components**; delete `liveInstance` and the `useSessions` call from the screen.
- [ ] **Step 4: Run `npx vitest run src/screens/agents`** — the list/form suites PASS.
- [ ] **Step 5: Commit** `web: agents list and form v4 (#639)`.

### Task 3: Agent card screen — two tabs, terminal, Start/Stop, send message

**Files:**
- Modify: `web/src/screens/agents/AgentScreen.tsx`, `web/src/screens/agents/InboxTab.tsx`, `web/src/screens/agents/agents.css`
- Delete: `web/src/screens/agents/DossierTab.tsx`, `web/src/screens/agents/MemoryTab.tsx`, `web/src/screens/agents/RunsTab.tsx`, `web/src/screens/agents/AgentMemoryQuestions.test.tsx`
- Test: `web/src/screens/agents/AgentScreen.test.tsx`, new `web/src/screens/agents/InboxTab.test.tsx`

**Interfaces:**
- Consumes: Task 1's hooks; `TermOverlay` from `../task/TermOverlay` with `{id: agent.id, tmux_name: agent.id}`.
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing tests** — header shows description/dir/command; with `session_alive: false` the actions offer **Start** and no Terminal; with `session_alive: true` they offer **Terminal** (opens the overlay) and **Stop**; the send-message field POSTs to `/messages`; only the tabs Inbox and Questions exist (no Dossier/Memory/Runs); the Inbox tab lists messages with from/body and an unread/read filter.
- [ ] **Step 2: Run `npx vitest run src/screens/agents`** — expect FAIL.
- [ ] **Step 3: Rewrite `AgentScreen` and `InboxTab`, delete the three tab files and the memory test**, prune the dead CSS blocks.
- [ ] **Step 4: Run `npx vitest run`** — the whole suite PASSes.
- [ ] **Step 5: Commit** `web: agent card v4 — inbox, threads, terminal, Start (#639)`.

### Task 4: Docs and full verification

**Files:**
- Modify: `docs/11-dashboard.md` (section «Экран 2b: Agents»)

- [ ] **Step 1: Rewrite the dashboard doc section** to the v4 screens: list (id, description, session dot, unread, open questions), card with header actions (send message, Terminal/Start, Stop, Enable/Disable, Edit, Delete) and two tabs (Questions, Inbox); the Wake/Dossier/Memory/Runs paragraphs are deleted.
- [ ] **Step 2: Run `npx tsc -b && npx vitest run && npm run build`** in `web/` — all green.
- [ ] **Step 3: Grep for leftovers**: `grep -rn "wake\|dossier\|memory\|subscriptions\|prompt_path\|inbox_queued" web/src` returns nothing agent-related.
- [ ] **Step 4: Commit and open the PR.**
