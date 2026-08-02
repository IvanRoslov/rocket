# Mobile agents screens — spec v4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Привести мобильные экраны агентов к спеке v4 (#639): реестр + инбокс сообщений + Q&A-треды + отправка сообщения + Start/Stop; Dossier/Prompt/Memory/Runs выпилены.

**Architecture:** Слой типов и хуков (`src/api/types.ts`, `src/api/queries.ts`) переводится на новые шейпы `/v1/agents*`; чистые презентационные хелперы (`src/lib/agents.ts`) теряют всё, что относилось к managed-слою; два экрана (`app/(tabs)/agents.tsx`, `app/agent/[id].tsx`) упрощаются до двух вкладок.

**Tech Stack:** Expo Router 57, React Native, @tanstack/react-query, jest + @testing-library/react-native, TypeScript.

## Global Constraints

- Форма агента: `{id, description, project, dir, command, enabled, session_alive, unread, open_questions, awaiting_user, created_at, updated_at}`.
- Инбокс: `{id, from, body, status: 'unread'|'read', created_at, read_at?}` из `GET /v1/agents/{id}/inbox`.
- Эндпоинты: `GET /v1/agents`, `GET /v1/agents/{id}`, `GET /v1/agents/{id}/inbox`, `POST /v1/agents/{id}/messages|start|stop|enable|disable`, Q&A — без изменений.
- `wake`, `items`, `memory`, `subscriptions`, `cron`, `prompt`, `prompt_path`, `agent`, `inbox_queued`, run-сессии `<id>-run-<n>` — удалены.
- Живость сессии берётся из `session_alive`, а не из списка сессий; id tmux-сессии агента == id агента.
- `npx tsc --noEmit` и `npm test` должны быть зелёными.

---

### Task 1: Типы и хуки API

**Files:**
- Modify: `mobile/src/api/types.ts` (блок Agent*)
- Modify: `mobile/src/api/queries.ts` (блоки agent-запросов и мутаций)
- Test: `mobile/src/api/agents.test.tsx`

**Interfaces:**
- Produces: `Agent`, `AgentInboxMessage`, `AgentQuestion`; хуки `useAgents(project?)`, `useAgent(id)`, `useAgentInbox(id, enabled)`, `useAgentQuestions(id)`, `useSendAgentMessage()`, `useStartAgent()`, `useStopAgent()`, `useSetAgentEnabled()`.

- [ ] Step 1: переписать тесты `agents.test.tsx` под новые урлы (`/v1/agents/sre/messages`, `/start`, `/stop`), убрать wake.
- [ ] Step 2: `npx jest src/api/agents.test.tsx` — падает.
- [ ] Step 3: обновить `types.ts` и `queries.ts`.
- [ ] Step 4: тест зелёный. Commit.

### Task 2: Презентационные хелперы

**Files:**
- Modify: `mobile/src/lib/agents.ts`
- Test: `mobile/src/lib/agents.test.ts`

**Interfaces:**
- Produces: `awaitingUser(agents)`, `agentBadges(a): BadgeProps[]`, `inboxStatusBadge(status)`.

- [ ] Step 1: тесты: бейджи `live`/`disabled`/`? N awaiting you`/`N open Q`/`N unread`/`idle`, `unread` ≠ `read` по цвету, удалить тесты kind/item/subscription/isRoleRun.
- [ ] Step 2: прогнать — падает.
- [ ] Step 3: сократить `agents.ts` до трёх функций.
- [ ] Step 4: зелено. Commit.

### Task 3: Экраны

**Files:**
- Modify: `mobile/app/(tabs)/agents.tsx`, `mobile/app/agent/[id].tsx`
- Test: `mobile/app/agent/agent-screens.test.tsx`

- [ ] Step 1: тесты: список показывает description и бейджи; карточка открывается на Questions, есть вкладка Inbox с сообщением, кнопка Start при мёртвой сессии и Open chat при живой.
- [ ] Step 2: прогнать — падает.
- [ ] Step 3: переписать экраны (2 вкладки, шапка со Start/Stop/enable, нижняя панель «отправить сообщение»).
- [ ] Step 4: `npx tsc --noEmit` + `npm test` зелёные. Commit.

### Task 4: Хвосты и PR

- [ ] Step 1: `src/api/events.test.ts` — заменить `agent.instance_spawned` на `agent.session_started`.
- [ ] Step 2: полный прогон tsc + jest, PR.
