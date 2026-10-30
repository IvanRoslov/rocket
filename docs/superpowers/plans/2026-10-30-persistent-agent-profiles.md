# Persistent Agent Model Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Позволить запускать постоянного агента через `rocket agent start <id> --profile <name>` и повторять последнюю конфигурацию без флагов.

**Architecture:** Регистрация агента хранит имя последнего `launch_profile`. Session Manager разрешает профиль через существующий реестр, запускает адаптер без `SetupWorkspace` и сохраняет снимок `agent/profile/model/effort`; пустой профиль оставляет прежний `dir + command` launcher.

**Tech Stack:** Go, Cobra, SQLite migrations, net/http, существующие `agent.Agent` adapters; новых зависимостей нет.

**Spec:** `docs/superpowers/specs/2026-10-30-persistent-agent-profiles-design.md`

## Global Constraints

- Только `--profile`; отдельные `--agent`, `--model`, `--effort` у `agent start` не добавлять.
- Не вызывать `SetupWorkspace`, не добавлять system prompt или first message постоянному агенту.
- Пустой сохранённый профиль должен сохранять старое поведение `dir + command`, включая shell при пустой команде.
- Ручной tmux adoption не должен перенимать `launch_profile` или его снимок.
- Дашборд и mobile не менять; тексты существующего русскоязычного CLI оставить на русском.
- Новых зависимостей и изменений глобальных настроек harness нет.

## Review Focus

- Невалидный новый профиль не должен перезаписывать прежний сохранённый выбор — `TestStartAgentRejectsUnknownOrDisabledProfile` в Task 2.
- Отключённый/удалённый сохранённый профиль не должен молча переключаться на legacy `command` — тот же тест в Task 2.
- Внешняя tmux-сессия не должна получать snapshot сохранённого профиля — `TestAdoptAgentSessionIgnoresSavedLaunchProfile` в Task 2.
- Правка профиля после запуска не должна менять snapshot живой сессии — `TestStartAgentWithProfileUsesAdapterAndSavesChoice` в Task 2.
- Старые клиенты с пустым телом `/start` должны продолжить работать — `TestStartAgentWithoutBodyUsesLegacyLauncher` в Task 3.

---

### Task 1: Хранение последнего профиля агента

**Files:**
- Create: `internal/store/migrations/0025_agent_launch_profile.sql`
- Modify: `internal/store/agents.go:14-29,41-44,94-100,145-159`
- Test: `internal/store/agents_test.go:108-130`

**Interfaces:**
- Produces: поле `store.Agent.LaunchProfile string`, сохраняемое в `agents.launch_profile TEXT NOT NULL DEFAULT ''`.
- Пустая строка означает legacy launcher; внешнего ключа к `model_profiles` нет, чтобы удаление профиля не стирало регистрацию агента.

- [ ] **Step 1: Добавить failing store-тест**

В `TestAddGetAgent` присвоить fixture `LaunchProfile: "claude-opus"` и проверить после `AddAgent`/`GetAgent`, что значение сохранилось. В существующем тесте обновления агента изменить профиль на `kimi-k3-1-pro`, выполнить `UpdateAgent`, перечитать запись и проверить новое значение.

- [ ] **Step 2: Запустить тест и убедиться в ожидаемом падении**

Run: `go test ./internal/store -run 'TestAddGetAgent|TestUpdateAgent' -count=1`
Expected: FAIL — `Agent` ещё не содержит `LaunchProfile`.

- [ ] **Step 3: Добавить миграцию и поле persistence**

В `0025_agent_launch_profile.sql` добавить только:

```sql
ALTER TABLE agents ADD COLUMN launch_profile TEXT NOT NULL DEFAULT '';
```

Добавить `LaunchProfile string` в `store.Agent`, включить `launch_profile` в `agentColumns`, INSERT, UPDATE и `scanAgent` в порядке после `command` и перед `enabled`.

- [ ] **Step 4: Запустить store-тесты**

Run: `go test ./internal/store -count=1`
Expected: PASS, включая повторное открытие базы и существующие миграции.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/0025_agent_launch_profile.sql internal/store/agents.go internal/store/agents_test.go
git commit -m "feat: store persistent agent launch profile"
```

### Task 2: Запуск постоянного агента через adapter

**Files:**
- Modify: `internal/session/agent.go:20-71,90-199`
- Test: `internal/session/agent_test.go`
- Modify: `internal/api/agents.go:494-504` только для приведения вызова к новой сигнатуре; полный API — Task 3.

**Interfaces:**
- Consumes: `store.Agent.LaunchProfile` из Task 1; `store.Store.GetModelProfile(name string) (store.ModelProfile, error)`; существующие `agent.Get(name)`, `launchArgs`, `shellJoin`, `mergeEnv`.
- Produces: `func (m *Manager) StartAgent(ctx context.Context, a store.Agent, requestedProfile string) (store.Session, error)`.
- Правило выбора: непустой `requestedProfile`, иначе `a.LaunchProfile`, иначе legacy.
- Produces для Task 3: ошибки валидации `profile_not_found`, `profile_disabled`, существующие `agent_unknown`, `agent_unavailable`; успешный `store.Session` содержит snapshot.

- [ ] **Step 1: Написать failing-тест запуска с профилем**

Добавить `TestStartAgentWithProfileUsesAdapterAndSavesChoice`:
- создать enabled profile `{Name:"deep", Agent:"fake", Model:"opus", Effort:"high"}` и зарегистрировать `a` со старой `Command:"claude"`;
- вызвать `StartAgent(ctx, a, "deep")`;
- проверить один `LaunchCommand` с `Kind:"agent"`, `SessionID:"sre"`, `WorktreePath:a.Dir`, `Model:"opus"`, `Effort:"high"` и пустыми prompts;
- проверить отсутствие новых `SetupWorkspace` calls, adapter command/env в `runtime.CreateSpec`, `ROCKET_SESSION_ID`, `ROCKET_SOCKET`;
- проверить `sess.Agent=="fake"`, `Profile=="deep"`, `Model=="opus"`, `Effort=="high"` и сохранённый `Agent.LaunchProfile=="deep"`;
- изменить model profile после запуска и проверить, что повторно прочитанный session snapshot остался `deep/opus/high`.

- [ ] **Step 2: Написать failing-тесты повторного запуска и ошибок**

Добавить:
- `TestStartAgentReusesSavedProfileWithLatestValues`: `a.LaunchProfile="deep"`, пустой `requestedProfile`; текущие model/effort профиля попадают в launch и snapshot.
- `TestStartAgentRejectsUnknownOrDisabledProfile`: unknown → `profile_not_found`, disabled → `profile_disabled`; runtime не создаётся, прежний `LaunchProfile` не меняется. Отдельно удалить сохранённый профиль и убедиться, что запуск без флага даёт `profile_not_found`, а не legacy command.
- `TestAdoptAgentSessionIgnoresSavedLaunchProfile`: у `a` заданы `LaunchProfile:"deep"` и `Command:"claude"`; adoption по-прежнему выбирает adapter из command и оставляет `Profile/Model/Effort` пустыми.
- Обновить существующие вызовы `StartAgent(ctx, a)` на третий аргумент `""`; legacy assertions не менять.

- [ ] **Step 3: Запустить тесты и убедиться в ожидаемом падении**

Run: `go test ./internal/session -run 'TestStartAgent|TestAdoptAgentSession' -count=1`
Expected: FAIL — новая сигнатура и profile launch ещё не реализованы.

- [ ] **Step 4: Реализовать разрешение профиля и launch**

В `StartAgent` сохранить текущие проверки enabled/dir/live. Затем:
1. Выбрать имя по правилу из Interfaces; пустое имя выполняет существующий legacy path без изменений.
2. Загрузить profile; `store.ErrNotFound` → `validationErr("profile_not_found", "no profile "+name)`, `!Enabled` → `validationErr("profile_disabled", "profile is disabled: "+name+"; choose another with --profile")`.
3. Получить adapter по `profile.Agent`, проверить `Available()` как в orchestrator launch.
4. Построить `agent.LaunchSpec{SessionID:a.ID, Kind:AgentSessionKind, ProjectID:a.ProjectID, WorktreePath:a.Dir, SocketPath:m.cfg.SocketPath(), Model:profile.Model, Effort:profile.Effort}`.
5. Получить argv через `launchArgs`, объединить `ag.Env(spec)` с базовым Rocket env и передать `shellJoin(argv)` в runtime. Не вызывать `SetupWorkspace` и не ставить `FirstMessage`.
6. Для явного `requestedProfile` сохранить его в `a.LaunchProfile` через `UpdateAgent` после всех проверок; невалидный выбор и `agent_live` не должны менять регистрацию.

Изменить внутренний helper на `upsertAgentSession(a store.Agent, agentName string, prof LaunchProfile)`. Profile launch передаёт `profile.Agent` и `LaunchProfile{Name:profile.Name, Model:profile.Model, Effort:profile.Effort}`; legacy/adoption передают `m.agentAdapter(a)` и zero `LaunchProfile`. Helper заполняет соответствующие поля `store.Session`.

В API временно передать `""` третьим аргументом, чтобы пакет компилировался до Task 3.

- [ ] **Step 5: Запустить session-тесты**

Run: `go test ./internal/session -count=1`
Expected: PASS; старые тесты legacy command, shell, live и adoption остаются зелёными.

- [ ] **Step 6: Commit**

```bash
git add internal/session/agent.go internal/session/agent_test.go internal/api/agents.go
git commit -m "feat: launch persistent agents with model profiles"
```

### Task 3: API и CLI сценарий `--profile`

**Files:**
- Modify: `internal/api/agents.go:14-35,95-128,491-512`
- Test: `internal/api/agents_test.go`
- Modify: `internal/cli/agent.go:170-215,279-307`
- Test: `internal/cli/agent_test.go`
- Modify: `docs/03-daemon-api.md:237-257`
- Modify: `docs/04-cli.md:306-324`
- Modify: `docs/05-state.md:287-295`
- Modify: `docs/10-agents.md:172-184,221-240`

**Interfaces:**
- Consumes: `Manager.StartAgent(ctx, a, requestedProfile)` и `Agent.LaunchProfile` из Tasks 1–2.
- API: `POST /v1/agents/{id}/start` принимает `{profile?: string}`; ответ агента содержит `launch_profile`.
- CLI: `rocket agent start <id> [--profile <name>]`; `rocket agent show` печатает `profile:     <launch_profile|->`.

- [ ] **Step 1: Написать failing API-тесты**

Добавить:
- `TestStartAgentAcceptsProfileAndReturnsSnapshot`: зарегистрировать агента с реальным `t.TempDir()` и enabled fake profile; POST `{"profile":"deep"}` → 200; сохранённый agent содержит `launch_profile:"deep"`, session содержит `fake/deep/opus/high`.
- `TestStartAgentRejectsUnknownProfile`: POST `{"profile":"missing"}` → 400 `profile_not_found`, runtime/session не создаются.
- `TestStartAgentWithoutBodyUsesLegacyLauncher`: агент с `t.TempDir()` и `Command:"claude"`, пустое тело → 200 и legacy session без profile snapshot.
- `TestAgentResponseIncludesLaunchProfile`: GET агента с сохранённым `deep` возвращает `launch_profile:"deep"`.

- [ ] **Step 2: Запустить API-тесты и убедиться в падении**

Run: `go test ./internal/api -run 'TestStartAgent|TestAgentResponseIncludesLaunchProfile' -count=1`
Expected: FAIL — request/response fields ещё не реализованы.

- [ ] **Step 3: Реализовать API**

Добавить `LaunchProfile string `json:"launch_profile"`` в `agentResponse` и заполнить его в `toAgentResponse`. В `handleStartAgent` декодировать optional JSON body в struct с `Profile string`; `io.EOF` и JSON `null` означают пустой profile, malformed JSON → 400 `bad_request`. Передать `req.Profile` в `StartAgent`. В success response добавить `agent`, `profile`, `model`, `effort` из returned session, сохранив прежние `id/status/dir`.

- [ ] **Step 4: Запустить API-тесты**

Run: `go test ./internal/api -count=1`
Expected: PASS.

- [ ] **Step 5: Написать failing CLI-тесты**

Использовать существующие `fakeDaemon` и `newUnixSocketTestServer`. Добавить узкие `agentClient`/`agentDial` test seams по образцу `newModelsCmdWith`, не меняя внешние команды:
- `TestAgentStartSendsProfile`: `start cto --profile claude-opus` отправляет POST `/v1/agents/cto/start` с телом `{"profile":"claude-opus"}`; без флага profile в теле нет.
- `TestAgentShowPrintsLaunchProfile`: ответы GET agent/inbox содержат `launch_profile:"claude-opus"` и пустой inbox; вывод содержит `profile:     claude-opus`.

- [ ] **Step 6: Запустить CLI-тесты и убедиться в падении**

Run: `go test ./internal/cli -run 'TestAgentStartSendsProfile|TestAgentShowPrintsLaunchProfile' -count=1`
Expected: FAIL — флаг, test seam и вывод ещё не реализованы.

- [ ] **Step 7: Реализовать CLI**

Ввести приватный `agentClient` с используемыми `Get`/`Post`, production dial через `connect(true)` и конструкторы `newAgentStartCmdWith`/`newAgentShowCmdWith`; старые конструкторы делегируют им. `start` добавляет `--profile` и передаёт поле только при непустом значении. `show` печатает сохранённый `launch_profile` через `dashIfEmpty`. Обновить usage strings на `rocket agent start <id> [--profile <name>]`.

- [ ] **Step 8: Обновить документацию**

Зафиксировать сохранение последнего профиля, snapshot, ошибки `profile_not_found/profile_disabled`, приоритет над legacy command и неизменный adoption. В `docs/05-state.md` добавить колонку `launch_profile`. Не описывать dashboard changes — их нет.

- [ ] **Step 9: Запустить CLI-тесты и commit**

Run: `go test ./internal/cli -count=1`
Expected: PASS.

```bash
git add internal/api/agents.go internal/api/agents_test.go internal/cli/agent.go internal/cli/agent_test.go docs/03-daemon-api.md docs/04-cli.md docs/05-state.md docs/10-agents.md
git commit -m "feat: expose persistent agent profile launch"
```

### Task 4: Полная проверка и живая приёмка

**Files:**
- Modify only if a reproduced failure belongs to this feature: файлы Tasks 1–3.

**Interfaces:**
- Consumes: полный CLI/API/Manager flow из Tasks 1–3.

- [ ] **Step 1: Запустить стандартные проверки проекта**

Run: `make test`
Expected: все Go и web tests PASS.

Run: `make build`
Expected: web build и `bin/rocket` собираются без ошибок; `git status` не содержит созданных проверкой ненужных файлов.

- [ ] **Step 2: Проверить реальный повторный запуск в изолированном Rocket home**

Использовать временные `ROCKET_HOME` и agent dir, реальный доступный harness и профиль с моделью по умолчанию, чтобы не менять существующую регистрацию пользователя:
1. Запустить daemon из нового `bin/rocket` с этим `ROCKET_HOME`.
2. `rocket agent add acceptance --dir <temp-dir> --command "printf legacy"`; убедиться, что start без профиля использует legacy path.
3. После `stop`: `rocket agent start acceptance --profile <available-profile>`; `agent show` и session JSON показывают выбранные `launch_profile/agent/model/effort`.
4. После `stop`: `rocket agent start acceptance` без флагов; проверить тот же adapter/profile и живой TUI через tmux.
5. `rocket agent attach acceptance --json` возвращает точную attach command для `acceptance`; в интерактивном терминале выполнить обычный attach и отключиться через `C-b d`.
6. Остановить acceptance и временный daemon, удалить только созданные временные ресурсы.

Expected: оба profile-запуска открывают один и тот же выбранный TUI без перенастройки; legacy запуск остаётся рабочим.

- [ ] **Step 3: Проверить доставку сообщения profile-агенту**

Отправить короткое сообщение живому acceptance через `rocket send acceptance "Reply with OK"` и дождаться подтверждения доставки/ответа в transcript. Expected: очередь использует adapter выбранного профиля; нет цикла redelivery из-за определения harness из старой `command`.

- [ ] **Step 4: Commit финальных исправлений, если они потребовались**

```bash
git add <only-files-changed-to-fix-reproduced-acceptance-failures>
git commit -m "fix: correct persistent agent profile launch"
```

Если исправлений нет, commit не создавать.
