# Rocket Mobile — Full App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the `mobile/` Expo prototype into a full-featured rocket mobile client: interactive WebSocket terminal, SSE-driven updates, complete action set (kill/restore/cancel/cleanup/move), project creation wizard, project & repo management, and a test suite.

**Architecture:** Keep the existing expo-router + TanStack Query structure. Add an SSE layer (react-native-sse) that invalidates query caches on daemon events, an xterm.js terminal inside react-native-webview that owns its own WebSocket to the daemon, and mutation hooks for every daemon action. New screens: project wizard, project settings. Tests via jest-expo + React Native Testing Library.

**Tech Stack:** Expo SDK 57, TypeScript, expo-router, @tanstack/react-query, react-native-sse, react-native-webview, @xterm/xterm (+fit addon, embedded into generated HTML), jest-expo, @testing-library/react-native.

## Global Constraints

- All daemon endpoints per `docs/03-daemon-api.md`; error envelope `{"error":{code,message}}`.
- Design language from `*.mobile.dc.html` mockups; tokens live in `mobile/src/theme.ts` — no new hardcoded colors outside it.
- Everything must run in Expo Go (no custom native code); `npx tsc --noEmit` and `npm test` must pass at every commit.
- Commits: conventional prefix `feat(mobile):` / `test(mobile):` etc.
- All list screens support pull-to-refresh; all mutations surface daemon errors to the user.

---

### Task 1: Test infrastructure + unit tests for existing lib

**Files:**
- Modify: `mobile/package.json` (jest-expo preset, test script)
- Create: `mobile/src/lib/format.test.ts`, `mobile/src/api/client.test.ts`, `mobile/src/servers/ServerContext.test.tsx`

**Interfaces:**
- Produces: `npm test` (jest-expo). Later tasks add `*.test.ts(x)` next to sources.

- [x] Install: `npx expo install jest-expo jest @types/jest -- --save-dev` plus `@testing-library/react-native`.
- [x] `package.json`: `"test": "jest"`, `"jest": {"preset": "jest-expo", "transformIgnorePatterns": [standard expo list]}`.
- [x] `format.test.ts`: cases for `ago` (just now / m / h / d), `bytes` (B/KB/MB/GB), `uptime`, `stripAnsi` (CSI colors, cursor moves, OSC titles), `sessionDot`/`sessionBadge` mapping table.
- [x] `client.test.ts`: mock global.fetch — ok JSON, error envelope → ApiError(code,message,status), non-JSON error body, timeout abort.
- [x] `ServerContext.test.tsx`: add/remove/setActive persists to AsyncStorage mock (official mock from @react-native-async-storage/async-storage/jest/async-storage-mock).
- [x] Run `npm test` → green; commit `test(mobile): jest-expo infra + lib unit tests`.

### Task 2: SSE event stream → cache invalidation

**Files:**
- Create: `mobile/src/api/events.ts`, `mobile/src/api/events.test.ts`
- Modify: `mobile/app/_layout.tsx` (mount hook), `mobile/src/api/queries.ts` (lower polling)

**Interfaces:**
- Produces: `useEventStream(): {connected: boolean}` — subscribes to `GET /v1/events/stream` via react-native-sse `EventSource`, maps event types to query-key invalidations:
  - `session.*` → `['sessions']`, `['system']`, `['task']`
  - `task.*` → `['tasks']`, `['task']`
  - `message.*` → `['messages']`, `['system']`
  - `pr.*` → `['tasks']`, `['task']`, `['sessions']`
  - `repo.*`, `workspace.*` → `['repos']`, `['system']`
- Invalidation matches by key prefix after baseUrl: `qc.invalidateQueries({predicate})`.
- With SSE connected, polling drops to slow safety-net values (30s lists, 60s system); terminal output keeps 2s.
- Export `parseEventType(type: string): string[]` (pure mapping) for tests.

- [x] `npm i react-native-sse`.
- [x] Implement `events.ts`: EventSource on `${baseUrl}/v1/events/stream`, message handler `JSON.parse(e.data)` → `parseEventType(ev.type)` → invalidate; auto-reconnect (react-native-sse built-in via `pollingInterval`); teardown on baseUrl change; `connected` state from open/error events.
- [x] Test `parseEventType` mapping and predicate matching (pure functions).
- [x] Mount in `_layout.tsx` inside ServerProvider child component; pass `connected` through a lightweight context `ConnectionContext`.
- [x] Reduce refetchIntervals in `queries.ts` when stream connected (accept param via context).
- [x] `npm test`, `tsc`, commit `feat(mobile): SSE event stream drives cache invalidation`.

### Task 3: Full mutation set — sessions, tasks, system

**Files:**
- Modify: `mobile/src/api/queries.ts`
- Create: `mobile/src/api/mutations.test.ts`

**Interfaces:**
- Produces hooks (all invalidate their query keys on success):
  - `useKillSession(): mutate({id, cleanup?: boolean})` → POST `/v1/sessions/{id}/kill[?cleanup=true]`
  - `useRestoreSession(): mutate(id)` → POST `/v1/sessions/{id}/restore`
  - `useCancelTask(): mutate(id)` → POST `/v1/tasks/{id}/cancel`
  - `useMoveTask(): mutate({id, status})` → PATCH `/v1/tasks/{id}` `{status}`
  - `useUpdateTask(): mutate({id, title?, description?})` → PATCH
  - `useSystemCleanup(): mutate()` → POST `/v1/system/cleanup`, returns `{killed_tmux, removed_worktrees}`
  - `useQuestionDismiss(): mutate(id)` → POST `/v1/questions/{id}/answer` `{dismiss:true}`

- [x] Write tests with mocked fetch asserting method+path+body per hook (renderHook + QueryClientProvider wrapper).
- [x] Implement hooks; run tests; commit `feat(mobile): mutation hooks for sessions, tasks, system`.

### Task 4: Wire actions into UI

**Files:**
- Modify: `mobile/app/task/[id].tsx` (task menu: cancel / move status / dismiss question; session sheet: kill/restore buttons)
- Modify: `mobile/app/(tabs)/system.tsx` (Cleanup button + result alert; orphan badges)
- Modify: `mobile/app/(tabs)/kanban.tsx` (long-press task card → move/cancel action sheet)
- Create: `mobile/src/components/ActionSheet.tsx` (simple modal list of actions, reused)

**Interfaces:**
- `ActionSheet({visible, title, actions: {label, destructive?, onPress}[], onClose})`.

- [x] Build ActionSheet styled like the sessions sheet (grabber, cards).
- [x] Task screen header gets `⋯` button → ActionSheet: Move to backlog/in_progress/review/done (PATCH), Cancel task (confirm via Alert, cascades kill).
- [x] Sessions sheet rows get `⋯`: Kill, Kill + cleanup, Restore (only for errored/killed).
- [x] Open question card: «Dismiss» ghost button (confirm) → useQuestionDismiss.
- [x] System header Cleanup button (destructive confirm) → result Alert with counts.
- [x] Manual test against live daemon, `tsc`, commit `feat(mobile): kill/restore/cancel/move/cleanup actions`.

### Task 5: Interactive terminal — xterm.js in WebView over WS

**Files:**
- Create: `mobile/scripts/gen-terminal-html.js` (node script: reads `node_modules/@xterm/xterm/lib/xterm.js`, `css/xterm.css`, `@xterm/addon-fit`, writes `mobile/src/terminal/terminalHtml.ts` — a `export const TERMINAL_HTML: string` self-contained page)
- Create: `mobile/src/terminal/protocol.ts` (special-key escape sequences map + `buildWsUrl(baseUrl, id, readonly)`)
- Create: `mobile/src/terminal/protocol.test.ts`
- Modify: `mobile/app/term/[id].tsx` (WebView terminal + key toolbar; snapshot fallback)
- Modify: `mobile/package.json` (script `gen:terminal`)

**Interfaces:**
- WS protocol (docs/03-daemon-api.md): binary frames both ways for bytes; text frames JSON `{type:"resize",cols,rows}`, `{type:"ping"}`.
- `buildWsUrl('http://h:p', id, ro)` → `ws://h:p/v1/sessions/{id}/term[?readonly=true]`.
- `SPECIAL_KEYS: {label, seq}[]` — Esc `\x1b`, Tab `\t`, Ctrl+C `\x03`, Ctrl+D `\x04`, ←`\x1b[D` ↑`\x1b[A` ↓`\x1b[B` →`\x1b[C`, Enter `\r`.
- HTML page API (via `postMessage`/`injectJavaScript`): RN→WebView `window.rocketTerm.connect(wsUrl)`, `window.rocketTerm.sendKey(seq)`; WebView→RN messages `{type:'status', value:'open'|'closed'|'error'}`.

- [x] `npx expo install react-native-webview`; `npm i @xterm/xterm @xterm/addon-fit`; write generator, run it (generated file committed), add npm script.
- [x] HTML: xterm + FitAddon, `ws.binaryType='arraybuffer'`, `term.onData → TextEncoder → ws.send`, `onmessage → term.write(new Uint8Array(data))`, fit + resize JSON on load/rotation, ping every 30s, dark theme (#161618 bg).
- [x] `term/[id].tsx`: WebView with `source={{html}}`, `originWhitelist=['*']`, inject connect on load; status pill in header (live/closed); key toolbar above keyboard (ScrollView of keys → sendKey); readonly toggle button (reconnect with `?readonly=true`); fallback to old snapshot view when WS errors.
- [x] Test `protocol.ts` (url build, key table). Manual test on device against live daemon.
- [x] `tsc`, `npm test`, commit `feat(mobile): interactive xterm terminal over WebSocket`.

### Task 6: New Project wizard

**Files:**
- Create: `mobile/app/project/new.tsx` (single-screen stepper: Name → Main repo → Linked repos → Review)
- Create: `mobile/src/components/RepoPicker.tsx`
- Modify: `mobile/src/api/queries.ts` (add `useGithubRepos(q)`, `useRegisterRepo`, `useCreateProject`)
- Modify: `mobile/app/(tabs)/index.tsx` (header ＋ button + dashed «Create project» card → `/project/new`)

**Interfaces:**
- `useGithubRepos(q: string)` → GET `/v1/github/repos?q=` → `{repos: GithubRepo[]}` (debounced input, enabled when github token present).
- `useRegisterRepo(): mutate({github?: string, path?: string, id?: string})` → POST `/v1/repos` → Repo.
- `useCreateProject(): mutate({id?, name, main, linked?})` → POST `/v1/projects`.
- `RepoPicker({selected, exclude, onPick})` — three sources: registered repos list, GitHub search (registers on pick), manual local path (registers on add).

- [x] Wizard state machine in one file (step index, name, mainRepoId, linkedIds); progress dots; Review step shows summary and creates: register missing repos → POST project → navigate to kanban of new project.
- [x] Handle errors per step inline (e.g. clone failures from POST /v1/repos).
- [x] `tsc`, manual run, commit `feat(mobile): new project wizard`.

### Task 7: Project settings & repo management

**Files:**
- Create: `mobile/app/project/[id]/settings.tsx`
- Modify: `mobile/src/api/queries.ts` (`useUpdateProject`, `useDeleteProject`, `useDeleteRepo`)
- Modify: `mobile/app/(tabs)/kanban.tsx` (⚙ header button → project settings)
- Modify: `mobile/app/(tabs)/settings.tsx` (Repositories section: register + remove unused)

**Interfaces:**
- `useUpdateProject(): mutate({id, name?, main?, linked?})` → PATCH `/v1/projects/{id}`
- `useDeleteProject(): mutate(id)` → DELETE (409-style errors surfaced verbatim)
- `useDeleteRepo(): mutate(id)` → DELETE `/v1/repos/{id}`

- [x] Settings screen per mockup «Project» section: name input + save, linked chips with ✕, add-linked via RepoPicker, danger zone Delete (disabled while error; show daemon message).
- [x] Repos section in global Settings: Register repo (RepoPicker) + Remove for repos not used in any project (compute from projects list).
- [x] `tsc`, commit `feat(mobile): project settings and repo management`.

### Task 8: UX hardening

**Files:**
- Create: `mobile/src/components/Toast.tsx` (context + host, `useToast().show(message, kind)`)
- Modify: all screens — pull-to-refresh (RefreshControl), mutation errors → toast, connection banner (SSE disconnected + query errors → thin amber strip «reconnecting…» under header)
- Create: `mobile/src/components/Toast.test.tsx`

- [x] Toast host in root layout; auto-hide 3.5s; kinds ok/error.
- [x] RefreshControl on Projects/Kanban/Task/System/Settings scroll views (refetch relevant queries).
- [x] Replace remaining silent `catch {}` and Alert-only errors with toasts.
- [x] `npm test`, `tsc`, commit `feat(mobile): toasts, pull-to-refresh, connection banner`.

### Task 9: Final verification + docs

**Files:**
- Create: `mobile/README.md` (setup, LAN config, scripts, architecture map)
- Modify: `docs/11-dashboard.md` (mobile section pointer) — short paragraph.

- [x] Full suite: `npm test`, `npx tsc --noEmit`, `npx expo export --platform ios`, launch on device, walk every screen against live daemon.
- [x] Write README; commit `docs(mobile): README and dashboard doc pointer`.

## Self-Review Notes

- Spec coverage: prototype non-goals now covered by Tasks 5 (terminal input/WS), 2 (SSE), 3–4 (kill/restore/cleanup/cancel), 6 (wizard). Auth remains out of scope (trusted LAN, spec unchanged).
- Types referenced (`GithubRepo`, `Repo`, `SystemCleanupResult`) already exist in `mobile/src/api/types.ts`.
- Generated `terminalHtml.ts` is committed so CI/devices don't need the gen step; regenerate via `npm run gen:terminal` after xterm upgrades.
