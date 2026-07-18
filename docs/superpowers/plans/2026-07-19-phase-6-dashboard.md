# Фаза 6 — Дашборд: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Браузерный дашборд rocket: наблюдение и управление проектами, канбаном задач, карточкой задачи со встроенным терминалом, системным экраном и настройками — целиком через публичный API демона.

**Architecture:** SPA `web/` (TypeScript + React + Vite), продакшен-сборка embed'ится в бинарь `rocketd` и раздаётся на `127.0.0.1:4477`; данные — только REST `/v1` + SSE `/v1/events/stream`. В Go добавляются три куска фазы 6: раздача статики, эндпоинт системного экрана `GET /v1/system`, WebSocket-терминал `/v1/sessions/{id}/term` (tmux attach в PTY). Экраны задач (Kanban, Task) до вливания фазы 3 разрабатываются поверх MSW-моков, контракт — `docs/03-daemon-api.md`; после вливания фазы 3 из main моки выключаются.

**Tech Stack:** React 18, TypeScript (strict), Vite 6, react-router-dom v7, @tanstack/react-query v5, @xterm/xterm + @xterm/addon-fit, react-markdown, vitest + @testing-library/react + msw v2. Go: stdlib `http.ServeMux` (как в `internal/api/server.go`), `github.com/coder/websocket`, `github.com/creack/pty`.

## Global Constraints

- **Дизайн нормативен:** `docs/design/*.dc.html` + `docs/design/SUMMARY.md`. Реализуемый экран обязан визуально соответствовать своему мокапу; токены (цвета/шрифты/радиусы) — только через CSS-переменные из `web/src/styles/tokens.css`, значения — из SUMMARY.md. Перед вёрсткой экрана исполнитель ЧИТАЕТ соответствующий `.dc.html` (разметка внутри `<x-dc>`, логика мокапа в `<script data-dc-script>`) и переносит структуру/стили.
- **Данные только из API демона** (`docs/03-daemon-api.md`): никакого доступа к базе/файлам из web. Всё, что умеет дашборд, умеет и curl.
- Ошибки API имеют форму `{"error":{"code":"...","message":"..."}}` — клиент обязан её парсить.
- `GET /v1/sessions|/v1/projects|/v1/repos` возвращают **голый массив**; `GET /v1/messages` → `{"messages":[...]}` и **требует `?session=`**; `GET /v1/events` → `{"events":[...]}`. SSE: `id:` = event id, `event:` = тип, `data:` = JSON `{id,ts,type,session_id?,data{}}`, пинг-коммент каждые 15s, catch-up по `Last-Event-ID`.
- Go-код следует стилю репозитория: роуты регистрируются в `NewHandler` (`internal/api/server.go:41`), ошибки через `writeErr` (`internal/api/errors.go`), таблицы — в `internal/store/migrations/`.
- Эндпоинты `/v1/tasks*`, `/v1/questions*` (фаза 3) и `/v1/settings`, `/v1/github/repos` (фаза 4) в main ещё отсутствуют: UI под них пишется по контракту из `docs/03-daemon-api.md` поверх MSW-моков и обязан вежливо переживать 404 (`not_found`) от живого демона.
- Коммиты частые, по одному на задачу минимум; сообщения в стиле репозитория: `feat(web): ...`, `feat(api): ...`, `fix(...)`.
- Тесты: web — `npm test` (vitest, jsdom, msw); Go — `go test ./...`. Задача не закрыта, пока её тесты не зелёные.
- Никаких новых зависимостей сверх перечисленных в Tech Stack без согласования с оркестратором плана.

## Структура файлов (итоговая)

```
web/
  package.json  vite.config.ts  tsconfig.json  index.html  embed.go  dist/index.html (плейсхолдер)
  src/
    main.tsx  App.tsx  routes.tsx
    styles/tokens.css  styles/base.css
    lib/api.ts  lib/types.ts  lib/sse.ts  lib/queries.ts  lib/format.ts
    mocks/handlers.ts  mocks/browser.ts  mocks/fixtures.ts
    components/{AppShell,Badge,Dot,Button,Card,Modal,Tabs,Segmented,SearchInput,RepoPicker,TermPanel,...}.tsx
    screens/projects/{ProjectsScreen,ProjectCard}.tsx
    screens/newproject/{WizardScreen,Step1Name,Step2Main,Step3Linked,Step4Review}.tsx
    screens/kanban/{KanbanScreen,Column,TaskCard,NewTaskModal,StartModal}.tsx
    screens/task/{TaskScreen,QuestionBanner,QuestionsTab,OverviewTab,DocsTab,JournalTab,MessagesTab,SessionRail,TermOverlay}.tsx
    screens/system/SystemScreen.tsx
    screens/settings/{SettingsScreen,GithubSection,ReposSection,ProjectSection,DaemonSection}.tsx
internal/api/{static.go,system.go,term.go} (+ server.go правки)
internal/daemon/... (прокидка зависимостей, если потребуется для /v1/system)
docs/03-daemon-api.md (дополнение: /v1/system)
```

---

## Часть A — не заблокировано фазами 3–4

### Task 1: Каркас web/ + токены + шапка

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/routes.tsx`, `web/src/styles/tokens.css`, `web/src/styles/base.css`, `web/src/components/AppShell.tsx`, `web/src/vitest.setup.ts`
- Test: `web/src/components/AppShell.test.tsx`

**Interfaces:**
- Produces: маршруты `/` (Projects), `/projects/new`, `/p/:projectId` (Kanban), `/p/:projectId/tasks/:taskId`, `/system`, `/settings`; компонент `<AppShell>` (шапка 52px: лого R + rocket, переключатель проекта, табы Projects/Kanban/System/Settings) с `<Outlet/>`; CSS-переменные `--bg`, `--surface`, `--surface-2`, `--border`, `--text`, `--text-2`, `--text-3`, `--accent`, `--ok`, `--warn`, `--err`, `--review`, `--font-ui`, `--font-mono` и пр. по SUMMARY.md.

- [ ] **Step 1:** `npm create vite@latest web -- --template react-ts`, затем `npm i react-router-dom @tanstack/react-query && npm i -D vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom msw`. В `package.json` скрипты: `"test": "vitest run", "test:watch": "vitest"`.
- [ ] **Step 2:** `vite.config.ts` — dev-прокси на демон и vitest-конфиг:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/v1': { target: 'http://127.0.0.1:4477', ws: true } } },
  test: { environment: 'jsdom', setupFiles: './src/vitest.setup.ts', globals: true },
})
```

- [ ] **Step 3:** Прочитать `docs/design/SUMMARY.md` и заполнить `tokens.css` (все цвета/шрифты/радиусы как переменные) и `base.css` (reset, `body{background:var(--bg);font-family:var(--font-ui);color:var(--text)}`).
- [ ] **Step 4:** Написать падающий тест `AppShell.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AppShell } from './AppShell'

test('шапка: лого и табы', () => {
  render(<MemoryRouter><AppShell /></MemoryRouter>)
  expect(screen.getByText('rocket')).toBeInTheDocument()
  for (const tab of ['Projects', 'System', 'Settings']) {
    expect(screen.getByRole('link', { name: tab })).toBeInTheDocument()
  }
})
```

- [ ] **Step 5:** `npm test` → FAIL (нет AppShell). Реализовать `AppShell.tsx` по шапке из любого мокапа (все 6 содержат одинаковую шапку — брать из `Projects.dc.html`): sticky 52px, лого-квадрат, `rocket` моно, справа NavLink-табы (активный — фон `#f0f0ee`); переключатель проекта — заглушка-кнопка (наполняется в Task 5). `routes.tsx` — `createBrowserRouter` с AppShell-layout и плейсхолдерами экранов (`<div>Projects</div>` и т.п.). `main.tsx` — `RouterProvider` + `QueryClientProvider`.
- [ ] **Step 6:** `npm test` → PASS; `npm run build` → успех. Commit: `feat(web): scaffold Vite+React app shell with design tokens`.

### Task 2: Типы API, клиент, SSE-хук, react-query

**Files:**
- Create: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/lib/sse.ts`, `web/src/lib/queries.ts`, `web/src/lib/format.ts`, `web/src/mocks/handlers.ts`, `web/src/mocks/fixtures.ts`
- Test: `web/src/lib/api.test.ts`, `web/src/lib/sse.test.ts`

**Interfaces:**
- Produces: типы `Session` (json-поля из `internal/api/sessions.go:14-31`: `id,kind,project_id,repo_id,feature_slug,parent_id?,agent,branch,worktree_path,tmux_name,state,activity?,activity_ts?,created_at,updated_at`; `state: 'spawning'|'running'|'done'|'killed'|'errored'`; `activity: 'active'|'ready'|'idle'|'waiting_input'|'blocked'|'exited'`), `Project` (`id,name,main,linked,live_sessions,tasks{backlog,in_progress,review,done},created_at`), `Repo`, `Message`, `RocketEvent`, а также контрактные (фаза 3/4) `Task`, `TaskDoc`, `TaskLogEntry`, `Question`, `QuestionMessage`, `GithubRepo`, `Settings`.
- Produces: `api.get<T>(path)`, `api.post<T>(path, body)`, `api.patch`, `api.del` — бросают `ApiError {code, message, status}`; `useEventStream(onEvent: (e: RocketEvent) => void)` — EventSource `/v1/events/stream` c reconnect; `wireInvalidation(queryClient)` — маппинг типов событий на инвалидации (`session.*`→`['sessions']`+`['projects']`, `message.*`→`['messages']`, `task.*`→`['tasks']`, `repo.clone_*`→`['repos']`); хуки `useProjects()`, `useSessions(filter?)`, `useRepos()`, `useMessages(sessionId)`, `useTasksBoard(projectId)`, `useTask(id)` и мутации.

- [ ] **Step 1:** Написать `types.ts` (все типы выше, поля snake_case как в API) и `fixtures.ts` — согласованный набор данных: проект `billing` (main `api`, linked `web`,`infra`), 4 задачи по колонкам, подзадачи с сессиями, вопрос Q3 в статусе open/awaiting-user, доки spec/plan, журнал, сообщения. Числа/тексты взять из мокапов (`#12 Billing v2` и т.д.), чтобы вёрстка совпадала с дизайном.
- [ ] **Step 2:** Тест `api.test.ts` (msw): успешный `GET /v1/projects` парсится; ошибка `{"error":{code,message}}` с 409 бросает `ApiError` с этими полями; `POST` шлёт JSON-тело.
- [ ] **Step 3:** `npm test` → FAIL. Реализовать `api.ts`:

```ts
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message) }
}
async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const payload = await res.json().catch(() => null)
    throw new ApiError(res.status, payload?.error?.code ?? 'unknown', payload?.error?.message ?? res.statusText)
  }
  return res.status === 204 ? (undefined as T) : res.json()
}
export const api = {
  get: <T>(p: string) => req<T>('GET', p),
  post: <T>(p: string, b?: unknown) => req<T>('POST', p, b),
  patch: <T>(p: string, b: unknown) => req<T>('PATCH', p, b),
  put: <T>(p: string, b: unknown) => req<T>('PUT', p, b),
  del: <T>(p: string) => req<T>('DELETE', p),
}
```

- [ ] **Step 4:** `sse.ts`: `useEventStream` на `EventSource('/v1/events/stream')`, `onmessage` не используется — демон шлёт типизированные события, поэтому подписка через `es.addEventListener` невозможна по произвольным типам; вместо этого использовать `fetch`-стрим? Нет: EventSource доставляет неизвестные типы только через named-listeners. Решение: демоновский SSE пишет `event: <type>` — в `sse.ts` держать список известных префиксов не нужно, использовать один общий обработчик через `es.onmessage` НЕ выйдет, поэтому подписаться на конкретные типы из `docs/03-daemon-api.md` (`session.spawned`, `session.state_changed`, `session.activity_changed`, `session.killed`, `session.restored`, `message.queued`, `message.delivered`, `message.failed`, `workspace.branch_collision`, `workspace.cleanup`, `reconcile.orphan_tmux`, `task.question_asked`, `task.question_replied`, `task.question_resolved`, `pr.opened`, `pr.ci_changed`, `pr.merged`, `repo.clone_started`, `repo.clone_done`, `repo.clone_failed`) — константа `EVENT_TYPES: string[]`, на каждый `addEventListener(type, h)`; `onerror` → закрыть и переоткрыть через 2s (браузер сам шлёт Last-Event-ID). Тест `sse.test.ts`: мок EventSource, проверка что listener навешан на все типы и колбэк получает распарсенный JSON.
- [ ] **Step 5:** `queries.ts`: хуки на react-query поверх `api` (ключи `['projects']`, `['sessions', filter]`, `['repos']`, `['messages', sessionId]`, `['tasks', projectId]`, `['task', id]`, `['system']`) + `wireInvalidation(queryClient)`; вызвать `useEventStream` в `App.tsx` и связать с инвалидацией. `format.ts`: `timeAgo(iso)` («12m ago»), `shortSession(name)`.
- [ ] **Step 6:** `npm test` → PASS. Commit: `feat(web): typed api client, SSE stream hook, query layer`.

### Task 3: UI-кит по мокапам

**Files:**
- Create: `web/src/components/{Badge,Dot,Button,Card,Modal,Tabs,Segmented,SearchInput,EmptyState}.tsx` (+ css)
- Test: `web/src/components/uikit.test.tsx`

**Interfaces:**
- Produces: `<Dot state>` (цветная точка 7px: `active|ready`→зелёный, `idle`→серый, `blocked|waiting_input`→жёлтый, `errored|exited`→красный, `spawning`→indigo, пульсация для active); `<Badge tone="neutral|indigo|ok|warn|err|review" mono?>`; `<Button variant="primary|secondary|danger" size="sm|md">` (primary — чёрная `#1a1a1c`); `<Card>`; `<Modal title onClose>` (оверлей `rgba(15,15,17,.55)` + blur); `<Tabs items activeId onChange>` (чёрное подчёркивание, счётчики, жёлтый вариант); `<Segmented>`; `<SearchInput>`; `<EmptyState icon title action>`.

- [ ] **Step 1:** Прочитать разметку компонентов в `Projects.dc.html` и `Task.dc.html` (бейджи, кнопки, табы, модалка терминала). Написать падающий тест `uikit.test.tsx`: рендер каждого компонента, проверка классов-тонов и текстов, `Modal` закрывается по клику в оверлей и по Escape.
- [ ] **Step 2:** `npm test` → FAIL. Реализовать компоненты со стилями из токенов (радиусы бейджей 5–7px, кнопок 7–10px, панелей 10–14px, моно-шрифт по флагу `mono`).
- [ ] **Step 3:** `npm test` → PASS. Commit: `feat(web): ui kit (badges, buttons, modal, tabs) per design tokens`.

### Task 4: Go — раздача статики дашборда

**Files:**
- Create: `web/embed.go`, `web/dist/index.html` (плейсхолдер, коммитится), `internal/api/static.go`
- Modify: `internal/api/server.go:71-73` (catch-all), `.gitignore` (игнорировать `web/dist/*` кроме плейсхолдера: `web/dist/*` + `!web/dist/index.html`), `web/vite.config.ts` (`build.emptyOutDir: true`)
- Test: `internal/api/static_test.go`

**Interfaces:**
- Produces: `package web` с `var Dist embed.FS` (`//go:embed all:dist`); `api.registerStaticRoutes(mux)` — раздаёт `web/dist` на `/`, SPA-fallback: любой не-`/v1`, не-файловый путь → `index.html`; `/v1/*` не затрагивается (404 JSON для неизвестных `/v1`-путей сохраняется).

- [ ] **Step 1:** `web/embed.go`:

```go
// Package web embeds the production dashboard build served by rocketd.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
```

`web/dist/index.html` — плейсхолдер `<!doctype html><title>rocket</title><p>dashboard build missing — run npm run build in web/</p>`.

- [ ] **Step 2:** Тест `static_test.go` (httptest поверх `NewHandler`): `GET /` → 200, `text/html`; `GET /p/billing/tasks/12` → 200 index.html (SPA fallback); `GET /v1/definitely-missing` → 404 JSON `not_found`; `GET /assets/nope.js` → 404.
- [ ] **Step 3:** `go test ./internal/api/` → FAIL. Реализовать `static.go`:

```go
package api

import (
	"io/fs"
	"net/http"
	"strings"

	"rocket/web" // подставить фактический module path из go.mod
)

func registerStaticRoutes(mux *http.ServeMux) {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(dist))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeErr(w, http.StatusNotFound, "not_found", "unknown route")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: маршруты клиента отдают index.html
		if strings.Contains(p, ".") {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
```

В `server.go` заменить текущий catch-all `mux.HandleFunc("/")` на `registerStaticRoutes(mux)`, а JSON-404 для `/v1` обеспечить отдельным `mux.HandleFunc("/v1/", ...)` с прежним телом (проверить, что зарегистрированные `/v1`-роуты выигрывают по специфичности у этого префиксного).
- [ ] **Step 4:** `go test ./...` → PASS. Ручная проверка: `npm run build` в `web/`, `go run ./cmd/rocket daemon run`, открыть `http://127.0.0.1:4477` — шапка дашборда. Commit: `feat(api): embed and serve dashboard static build with SPA fallback`.

### Task 5: Экран Projects + переключатель проекта

**Files:**
- Create: `web/src/screens/projects/ProjectsScreen.tsx`, `web/src/screens/projects/ProjectCard.tsx`, `web/src/components/ProjectSwitcher.tsx`
- Modify: `web/src/routes.tsx`, `web/src/components/AppShell.tsx` (живой переключатель)
- Test: `web/src/screens/projects/ProjectsScreen.test.tsx`

**Interfaces:**
- Consumes: `useProjects()`, `useSessions()`, типы `Project`.
- Produces: `/` — сетка карточек по `Projects.dc.html`; клик по карточке → `/p/:projectId`; `ProjectSwitcher` в шапке (дропдаун 280px с поиском, зелёная точка при live>0), выбранный проект хранится в URL-параметре, табы Kanban в шапке ведут на `/p/<текущий>`.

- [ ] **Step 1:** Прочитать `Projects.dc.html`. Падающий тест (msw отдаёт fixtures): рендер `/` показывает карточку «Billing» с моно-бейджем `billing`, строкой `⌂ api  + web, infra`, бейджем `● 5 live`; пустой список → EmptyState с кнопкой «Create project»; клик по «＋ New project» ведёт на `/projects/new`.
- [ ] **Step 2:** `npm test` → FAIL. Реализовать: сетка `auto-fill minmax(340px,1fr)`, карточка (точка живости, стат-бейджи из `tasks{}` — нули до фазы 3 это валидное состояние, «idle» серый бейдж), футер `updated Xm ago` из `created_at`/данных сессий, сигнал «? awaiting you» рендерится по полю, которое появится с фазой 3 (`awaiting_questions?: number` — optional в типе, скрыт при отсутствии), пунктирная «＋ Create project».
- [ ] **Step 3:** `npm test` → PASS; визуальная сверка с мокапом в браузере (dev-сервер + живой демон; допускается `VITE_MOCKS=1`). Commit: `feat(web): projects screen and project switcher`.

### Task 6: Go — `GET /v1/system` + cleanup осиротевших tmux

**Files:**
- Create: `internal/api/system.go`
- Modify: `internal/api/server.go` (маршруты + зависимости в `Deps`), `internal/session/manager.go` (методы инспекции, если отсутствуют), `docs/03-daemon-api.md` (раздел «Система»)
- Test: `internal/api/system_test.go`

**Interfaces:**
- Consumes: store (сессии, сообщения), runtime tmux (`ListSessions` с префиксом `rocket-`), конфиг (пути, порт), `internal/version`.
- Produces:
  - `GET /v1/system` → `{"daemon":{"version","uptime_s","port","socket","db_path","config_path"},"queue":{"queued":N,"failed":N},"tmux":[{"name","session_id?","orphan":bool}],"worktrees":[{"path","session_id?","size_bytes","orphan":bool}],"log_tail":["...строки rocketd.log..."]}`. Orphan tmux — есть в tmux с префиксом rocket, нет живой сессии в store (и наоборот worktree без записи).
  - `POST /v1/system/cleanup` → `{killed_tmux:[names], removed_worktrees:[paths]}` — убивает только осиротевшее.
- Дополнить `docs/03-daemon-api.md` таблицей этих двух роутов.

- [ ] **Step 1:** Изучить `internal/runtime/tmux.go`, `internal/queue`, `internal/workspace` — какие методы листинга уже есть; добить недостающие (например `runtime.ListManaged() ([]string, error)`, `store.CountMessagesByStatus() (map[string]int, error)`, `workspace.List() ([]WorktreeInfo, error)` с размером через `filepath.WalkDir`).
- [ ] **Step 2:** Падающий тест `system_test.go` с фейковыми зависимостями: ответ содержит очередь `{queued:2,failed:1}` из подготовленных сообщений, tmux-сирота помечен `orphan:true`, `log_tail` ограничен 200 строками; `POST /v1/system/cleanup` не трогает не-сирот.
- [ ] **Step 3:** `go test ./internal/api/` → FAIL → реализовать `system.go` (хендлеры + сбор данных, лог-хвост читать с конца файла лога из конфига lumberjack, максимум 64KB).
- [ ] **Step 4:** `go test ./...` → PASS. Commit: `feat(api): system overview endpoint and orphan cleanup`.

### Task 7: Экран System

**Files:**
- Create: `web/src/screens/system/SystemScreen.tsx`
- Modify: `web/src/lib/{types,queries}.ts` (`SystemInfo`, `useSystem()` с `refetchInterval: 5000`), `web/src/mocks/handlers.ts`
- Test: `web/src/screens/system/SystemScreen.test.tsx`

**Interfaces:**
- Consumes: `GET /v1/system`, `useSessions()`, `POST /v1/sessions/{id}/kill?cleanup=`, `POST /v1/system/cleanup`.
- Produces: `/system` по `System.dc.html`: 4 стат-карточки (Live sessions, Agents running, Orphans — жёлтым при >0, Queue depth), таблица сессий с бейджами состояний + orphan-строки, панель очереди (queued/failed, красная моно-плашка последнего failed из `useMessages`-данных события), worktrees с размерами и суммой, панель Daemon, тёмный лог-хвост, кнопки «⌦ Cleanup orphans» и kill/cleanup на строках (с confirm-модалкой).

- [ ] **Step 1:** Прочитать `System.dc.html`; падающий тест: рендер по мок-данным, orphan подсвечен, клик Cleanup вызывает `POST /v1/system/cleanup` (проверить через msw-спай).
- [ ] **Step 2:** FAIL → реализовать → PASS. Commit: `feat(web): system observability screen`.

### Task 8: Go — WebSocket-терминал `/v1/sessions/{id}/term`

**Files:**
- Create: `internal/api/term.go`
- Modify: `go.mod` (`github.com/coder/websocket`, `github.com/creack/pty`), `internal/api/server.go` (роут `GET /v1/sessions/{id}/term`)
- Test: `internal/api/term_test.go`

**Interfaces:**
- Consumes: `Manager.AttachCommand(id)` (`internal/session/manager.go:358` → argv `["tmux","attach","-t","=rocket-<name>"]`), состояние сессии из store.
- Produces: протокол из `docs/03-daemon-api.md`: server→client бинарные фреймы (вывод PTY); client→server бинарные (ввод) и текстовые JSON `{type:"resize",cols,rows}` / `{type:"ping"}` (ответ `{type:"pong"}`); `?readonly=true` — ввод игнорируется; сессия не в `spawning|running` → 409 `session_not_live`; закрытие WS убивает только attach-клиент.

- [ ] **Step 1:** `go get github.com/coder/websocket github.com/creack/pty`.
- [ ] **Step 2:** Падающий тест: юнит на `parseControl([]byte) (ctrl, ok)` (resize/ping/мусор) и хендлер-тест: несуществующая сессия → 404, мёртвая → 409 (WS-часть с настоящим tmux в юнит не тащить).
- [ ] **Step 3:** FAIL → реализовать `term.go`:

```go
func (h *handler) handleSessionTerm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := h.deps.Store.GetSession(id) // фактические имена взять из sessions.go
	// 404/409 как в Interfaces
	argv, err := h.deps.Manager.AttachCommand(id)
	conn, err := websocket.Accept(w, r, nil)
	defer conn.CloseNow()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	defer func() { ptmx.Close(); cmd.Process.Kill(); cmd.Wait() }()
	readonly := r.URL.Query().Get("readonly") == "true"
	ctx := r.Context()
	go func() { // pty -> ws
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil { return }
			}
			if err != nil { conn.Close(websocket.StatusNormalClosure, "session ended"); return }
		}
	}()
	for { // ws -> pty
		typ, data, err := conn.Read(ctx)
		if err != nil { return }
		switch typ {
		case websocket.MessageBinary:
			if !readonly { ptmx.Write(data) }
		case websocket.MessageText:
			if c, ok := parseControl(data); ok {
				switch c.Type {
				case "resize": pty.Setsize(ptmx, &pty.Winsize{Cols: c.Cols, Rows: c.Rows})
				case "ping":   conn.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				}
			}
		}
	}
}
```

(точные имена полей/методов Deps сверить с `server.go`; таймауты чтения не ставить — терминал живёт долго, keepalive — ping/pong).
- [ ] **Step 4:** `go test ./...` → PASS. Ручная проверка: `rocket spawn`-сессия (или любая rocket-tmux), `websocat` бинарным режимом — виден вывод tmux. Commit: `feat(api): websocket pty terminal attach for sessions`.

### Task 9: TermPanel (xterm.js) + оверлей терминала

**Files:**
- Create: `web/src/components/TermPanel.tsx`, `web/src/screens/task/TermOverlay.tsx`
- Modify: `web/package.json` (`@xterm/xterm`, `@xterm/addon-fit`)
- Test: `web/src/components/TermPanel.test.tsx` (юнит на url/протокол-хелперы `termUrl(sessionId, readonly)` → `ws(s)://host/v1/sessions/id/term`, энкодинг resize-сообщения)

**Interfaces:**
- Consumes: WS из Task 8.
- Produces: `<TermPanel sessionId readonly?>` — xterm с темой мокапа (фон `#161618`, зелёный курсор), FitAddon, `binaryType='arraybuffer'`, ввод → бинарные фреймы, `onResize` → `{type:"resize"}`, ping каждые 30s, авто-reconnect с бэннером «reconnecting…»; `<TermOverlay session onClose>` — оверлей по `Task.dc.html` (тёмная панель, шапка: имя сессии, «tmux · live attach», `80×24` — актуальные cols×rows, кнопка attach-copy, Close; клик по фону закрывает).

- [ ] **Step 1:** Падающий юнит-тест хелперов → FAIL → реализовать компонент → PASS.
- [ ] **Step 2:** Ручная проверка через Playwright/браузер на живом демоне: открыть оверлей, ввести команду в shell tmux-сессии, увидеть вывод; ресайз окна меняет геометрию. Commit: `feat(web): embedded xterm terminal panel and overlay`.

### Task 10: Визард создания проекта

**Files:**
- Create: `web/src/screens/newproject/{WizardScreen,Step1Name,Step2Main,Step3Linked,Step4Review}.tsx`, `web/src/components/RepoPicker.tsx`
- Modify: `web/src/mocks/handlers.ts` (`GET /v1/github/repos`, `PUT /v1/settings` по контракту фазы 4)
- Test: `web/src/screens/newproject/Wizard.test.tsx`

**Interfaces:**
- Consumes: `GET /v1/projects` (уникальность id), `GET /v1/repos`, `POST /v1/repos` (`{path}` или `{github:"owner/name"}`), `POST /v1/projects` (`{id,name,main,linked}`), контрактные `GET /v1/github/repos?q=`, SSE `repo.clone_*`.
- Produces: `/projects/new` по `NewProject.dc.html`: степпер 4 шагов; Шаг 1 — Name + авто-slug id (`[a-z0-9-]`, транслит/lowercase, редактируем, инлайн «● available»/«taken»); Шаг 2 — `RepoPicker` (сегмент GitHub/Registered/Local path; GitHub: при 404/`github_token_missing` — placeholder «Connect GitHub» с инлайн-формой токена `PUT /v1/settings`, при успехе список с поиском; выбор GitHub-репо → `POST /v1/repos {github}` с прогрессом клона по SSE и retry при `clone_failed`; Local: путь → `POST /v1/repos {path}`, зелёная панель origin/default branch из ответа); Шаг 3 — тот же пикер в multi-режиме, main задизейблен, «Skip»; Шаг 4 — сводка → `POST /v1/projects` → навигация `/p/<id>`.

- [ ] **Step 1:** Прочитать `NewProject.dc.html`. Падающие тесты: генерация slug из «Биллинг v2» → `billing-v2`; занятый id подсвечен; happy-path по Registered-репо до `POST /v1/projects` (msw-спай тела запроса); GitHub-таб без токена показывает «Connect GitHub».
- [ ] **Step 2:** FAIL → реализовать (`RepoPicker` — переиспользуемый: props `mode:'single'|'multi'`, `exclude:[ids]`, `onSelect`) → PASS. Commit: `feat(web): project creation wizard with repo picker`.

### Task 11: Settings

**Files:**
- Create: `web/src/screens/settings/{SettingsScreen,GithubSection,ReposSection,ProjectSection,DaemonSection}.tsx`
- Test: `web/src/screens/settings/Settings.test.tsx`

**Interfaces:**
- Consumes: `GET/PATCH/DELETE /v1/repos*`, `GET/PATCH/DELETE /v1/projects*`, `GET /v1/system` (Daemon-секция), контрактные `GET/PUT /v1/settings`.
- Produces: `/settings` по `Settings.dc.html`: grid `180px 1fr`, sticky-меню. GitHub: форма токена (маскировка после сохранения, плашка «Authorized as @…» из ответа PUT; при 404 от демона — жёлтая заметка «появится с фазой GitHub»). Repositories: таблица реестра (`used in` считается по `projects.main/linked`), правка env/symlinks/post_create в модалке (`PATCH /v1/repos/{id}`), Remove активен только для незанятых. Project: переименование, чипы main/linked (`PATCH /v1/projects/{id}`), danger-зона Delete (disabled, если есть незакрытые задачи/живые сессии, с пояснением). Daemon: read-only из `/v1/system`.

- [ ] **Step 1:** Прочитать `Settings.dc.html`; падающие тесты: Remove задизейблен у занятого репо; PATCH проекта шлёт правильное тело; GitHub-секция переживает 404.
- [ ] **Step 2:** FAIL → реализовать → PASS. Commit: `feat(web): settings screens (github, repos, project, daemon)`.

### Task 12: Kanban (поверх моков фазы 3)

**Files:**
- Create: `web/src/screens/kanban/{KanbanScreen,Column,TaskCard,NewTaskModal,StartModal}.tsx`, `web/src/mocks/browser.ts`
- Modify: `web/src/main.tsx` (условный запуск msw-worker при `import.meta.env.VITE_MOCKS`), `web/src/mocks/handlers.ts` (полные хендлеры `/v1/tasks*` по `docs/03-daemon-api.md`: board, POST, PATCH move, start)
- Test: `web/src/screens/kanban/Kanban.test.tsx`

**Interfaces:**
- Consumes: контракт `GET /v1/tasks?project=&board=true` (форма ответа моков: `{columns:{backlog:[Task],in_progress:[...],review:[...],done:[...],cancelled:[...]}}` — при интеграции сверить с фактической и поправить ОДИН адаптер `lib/queries.ts#useTasksBoard`), `PATCH /v1/tasks/{id} {status}`, `POST /v1/tasks`, `POST /v1/tasks/{id}/start {agent?}`, `useSessions({project})` для строк живости.
- Produces: `/p/:projectId` по `Kanban.dc.html`: 4 колонки + фильтр Cancelled, поиск, карточка задачи (repo-строка `main → workers-repos`, orch-живость из сессии, PR-бейджи из подзадач — поля контрактные, скрыты при отсутствии, сигналы, Start в Backlog с модалкой выбора агента), нативный HTML5 drag-n-drop (dragstart → opacity .4, dragover колонки → бордер `#c7d2fe`, drop → мутация move с оптимистичным обновлением и откатом на ошибке), «＋» в Backlog → NewTaskModal (`title` + markdown `description`).

- [ ] **Step 1:** Прочитать `Kanban.dc.html`. Падающие тесты: рендер колонок с корректным распределением фикстур; поиск фильтрует; drop-хендлер зовёт PATCH со статусом колонки; Start шлёт POST start.
- [ ] **Step 2:** FAIL → реализовать → PASS. Проверка вживую: `VITE_MOCKS=1 npm run dev` — канбан соответствует мокапу. Commit: `feat(web): kanban board with dnd over phase-3 API contract (mocked)`.

### Task 13: Карточка задачи (поверх моков фазы 3)

**Files:**
- Create: `web/src/screens/task/{TaskScreen,QuestionBanner,QuestionsTab,OverviewTab,DocsTab,JournalTab,MessagesTab,SessionRail}.tsx`
- Modify: `web/src/mocks/handlers.ts` (`/v1/tasks/{id}`, `/docs`, `/log`, `/questions`, `/v1/questions/{id}/reply|answer`)
- Test: `web/src/screens/task/Task.test.tsx`

**Interfaces:**
- Consumes: контрактные `GET /v1/tasks/{id}` (+`docs`,`log`,`questions`), `POST /v1/questions/{id}/reply {body}`, `POST /v1/questions/{id}/answer {body}|{dismiss:true}`; реальные уже сейчас: `GET /v1/messages?session=`, `POST /v1/messages {to,body}`, `GET /v1/sessions`, kill/restore, `GET /v1/sessions/{id}/attach`, TermOverlay из Task 9.
- Produces: `/p/:projectId/tasks/:taskId` по `Task.dc.html`: заголовок с бейджем статуса и метой; жёлтый баннер открытых вопросов; табы: **Questions** (треды: жёлтая шапка «чья очередь» — производная от автора последней реплики, сворачиваемый context, реплики с аватарами O/Y и статусом доставки из `/v1/messages`-статусов, textarea + «Clarify — keep open» / «Answer & close» / «Dismiss», resolved свёрнуты), **Overview** (markdown-описание через react-markdown, дерево подзадач со статусами/PR, final report при наличии), **Docs** (карточки kind+версия, рендер markdown), **Journal** (таймлайн с фильтрами по kind/автору, цветные точки: decision indigo, problem красный, note серый, status зелёный), **Messages** (чат: свои справа indigo, оркестратор слева, статус доставки, sticky-ввод → `POST /v1/messages` оркестратору). Правый рейл: карточки Orchestrator/Workers (Dot активности, term ▣ → TermOverlay, attach ⧉ → clipboard `rocket attach <id>` + тултип «copied», kill с confirm, restore для errored).

- [ ] **Step 1:** Прочитать `Task.dc.html` полностью (это самый насыщенный мокап). Падающие тесты: баннер виден при open-вопросе и скрыт без; «Answer & close» шлёт `POST .../answer` и убирает тред из открытых; отправка сообщения зовёт `POST /v1/messages` с `to=<session оркестратора>`; attach копирует команду (мок clipboard).
- [ ] **Step 2:** FAIL → реализовать (компонент на таб, общий `TaskScreen`-layout 820+312) → PASS. Commit: `feat(web): task card screen with q&a, docs, journal, messages, session rail`.

---

## Часть B — после вливания фазы 3 (сигнал от пользователя)

### Task 14: Интеграция с реальной фазой 3

**Files:**
- Modify: `web/src/lib/{types,queries}.ts`, `web/src/mocks/handlers.ts`, экраны Kanban/Task/Projects по результатам сверки
- Test: прогон всех существующих тестов + новые на расхождения

**Interfaces:**
- Consumes: фактические `/v1/tasks*`, `/v1/questions*` из main.

- [ ] **Step 1:** `git fetch && git merge origin/main` в ветку дашборда; разрешить конфликты (ожидаемые: `server.go` — новые роуты фазы 3 рядом со static/system/term; `go.mod`).
- [ ] **Step 2:** Прочитать фактические `internal/api/tasks.go` (+auth.go) вливённой фазы 3: json-теги, форма board-ответа, имена полей вопросов/реплик, как считается «чья очередь». Составить список расхождений контракта с `lib/types.ts`/моками.
- [ ] **Step 3:** Обновить типы, `useTasksBoard`-адаптер, msw-хендлеры (тесты остаются на msw — теперь зеркалящем реальность), поправить экраны. Включить настоящие счётчики задач и бейдж «? awaiting you» на карточках проектов (`GET /v1/projects` фазы 3 отдаёт реальные `tasks{}`).
- [ ] **Step 4:** `npm test` и `go test ./...` → PASS. Живой прогон: создать задачу из канбана, Start (реальный оркестратор), пронаблюдать SSE-движение карточки. Commit: `feat(web): wire kanban and task screens to live phase-3 API`.

### Task 15: E2E-верификация и финализация

**Files:**
- Modify: `docs/11-dashboard.md` (отметки о реализации, если расходится), `docs/03-daemon-api.md` (финальная сверка), `README`/`docs/04-cli.md` при необходимости (как собирать web)
- Test: чек-лист ниже через Playwright MCP на живом демоне

- [ ] **Step 1:** Полный цикл из критерия готовности фазы 6, из браузера без локального терминала: создать проект визардом (Local path-репо) → создать задачу → Start → наблюдать оркестратора → написать ему в Messages → открыть встроенный терминал, поработать в нём → дождаться вопроса/ответить (или сэмулировать через CLI) → перевести в done. Зафиксировать скриншоты каждого шага.
- [ ] **Step 2:** Использовать skill superpowers:verification-before-completion; `npm test`, `go test ./...`, `npm run build` + `go build ./...` — всё зелёное с чистым `git status`.
- [ ] **Step 3:** Commit + skill superpowers:finishing-a-development-branch (merge/PR по выбору пользователя).

---

## Self-Review (выполнено при написании)

- Покрытие спеки: все пункты фазы 6 роадмапа замаплены: web/-каркас (T1–3), статика (T4), Проекты+визард+Settings (T5, T10, T11), Канбан (T12), Карточка задачи (T13), встроенный терминал (T8–9), Система (T6–7), критерий готовности (T15). GitHub-каталог в визарде — контрактный UI (фаза 4 делает бэкенд).
- Известные контрактные риски вынесены в Task 14 (форма board-ответа, поля вопросов) — сверка по факту вливания, адаптация в одном адаптере.
- Типы согласованы: `Session.state/activity` — из аудита кода; формы ответов list-эндпоинтов — из аудита (голые массивы vs обёртки) и повторены в Global Constraints.
