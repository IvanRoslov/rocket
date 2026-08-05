# План: статус задачи `brainstorm` (сторона демона)

> Задача #1077, подзадача #1078. Реализуется по TDD: сначала падающий тест, потом код.

**Цель:** добавить статус `brainstorm` между `backlog` и `in_progress`, два автоматических
перехода демона и осведомлённость всех серверных поверхностей о новом статусе.

**Канонический порядок:** `backlog → brainstorm → in_progress → review → done`, плюс `cancelled`.
Строковое значение — ровно `brainstorm`, ярлык колонки CLI — `BRAINSTORM`.

## Глобальные ограничения

- Миграции БД нет: `tasks.status` — свободный `TEXT`.
- Ограничений на переходы нет: ручной `rocket task move` / `PATCH {status}` принимает
  `brainstorm` наравне с прочими, и явное действие человека всегда побеждает.
- Подзадачи-воркеры никогда не попадают в `brainstorm` — они спавнятся сразу в работу.
- Майлстоны: статус допустим, но автоматики нет, их двигает постоянный агент руками.
- `web/` и `mobile/` не трогаем — их делают другие воркеры.
- `internal/ghpoller/reactions.go` не трогаем: он работает только с подзадачами воркеров.

## Структура изменений

Общий словарь «активных» статусов живёт в `internal/store/tasks.go` рядом с
`validTaskStatuses` — это единственное место, где статусы уже перечислены как данные.
Общий обход двух запросов для майлстонов живёт в `internal/heartbeat/quiet.go` рядом с
`QuietMilestone`: правило «активный майлстон» читают и heartbeat, и API, ровно как
и само правило тишины, поэтому и делить его надо там же.

| Файл | Ответственность изменения |
|---|---|
| `internal/store/tasks.go` | `brainstorm` в `validTaskStatuses`; `ActiveTaskStatuses` + `IsActiveTaskStatus` |
| `internal/api/tasks.go` | `startTask` ставит `brainstorm`; `Brainstorm` в `taskBoard` и `toTaskBoard` |
| `internal/api/sessions.go` | после спавна воркера корневая задача `brainstorm → in_progress` |
| `internal/api/projects.go` | счётчик `brainstorm` в `taskCounters` |
| `internal/api/quiet.go` | набор тихих майлстонов по всем активным статусам |
| `internal/cli/task.go` | колонка `BRAINSTORM` в `renderTaskBoard` |
| `internal/heartbeat/heartbeat.go` | `tickOne` не отсекает задачу в `brainstorm` |
| `internal/heartbeat/quiet.go` | `QuietMilestone` + `ActiveMilestones` для обхода двух статусов |
| `docs/*.md` | списки статусов, таблицы переходов, порог тишины |

## Задачи

### Задача 1: store — валидный статус и словарь активных статусов

**Файлы:** изменить `internal/store/tasks.go:12`; тест `internal/store/tasks_test.go`.

**Производит:** `store.ActiveTaskStatuses []string` (порядок `brainstorm`, `in_progress`) и
`store.IsActiveTaskStatus(status string) bool` — их используют задачи 4 и 5.

- [ ] Тест: `AddTask` со `Status: "brainstorm"` проходит; `IsActiveTaskStatus` истинна для
      `brainstorm` и `in_progress` и ложна для `backlog`/`review`/`done`/`cancelled`.
- [ ] Убедиться, что тест падает.
- [ ] Добавить `"brainstorm": true`, `ActiveTaskStatuses`, `IsActiveTaskStatus`.
- [ ] Тесты зелёные, коммит.

### Задача 2: `task start` останавливается в `brainstorm`

**Файлы:** изменить `internal/api/tasks.go:949-963`; тест `internal/api/tasks_test.go`.

- [ ] Тест: `POST /v1/tasks/{id}/start` оставляет задачу в `brainstorm`, событие шины
      `task.status_changed` несёт `to: "brainstorm"`, в журнале строка
      `status: backlog → brainstorm (by system)`. Поправить существующие тесты, которые
      ждут `in_progress` после старта.
- [ ] Убедиться, что тест падает; заменить литерал; тесты зелёные; коммит.

### Задача 3: первый спавн воркера двигает корень в `in_progress`

**Файлы:** изменить `internal/api/sessions.go:260-271`; тест `internal/api/sessions_test.go`.

- [ ] Тест: корень в `brainstorm` + `POST /v1/sessions` (воркер) → корень в `in_progress`
      с записью в журнале; повторный спавн статус не меняет; корень, вручную переведённый
      в `review`, остаётся в `review`.
- [ ] Убедиться, что тест падает.
- [ ] После успешного `UpdateTask(sub)` добавить: если `root.Status == "brainstorm"`, вызвать
      `applyTaskStatusChange(d, root, caller, "in_progress")`. Идемпотентность даёт сама
      проверка статуса — `root` читается из БД в начале обработчика.
- [ ] Тесты зелёные, коммит.

### Задача 4: поверхности чтения — доска API, счётчики, доска CLI

**Файлы:** изменить `internal/api/tasks.go:137-178`, `internal/api/projects.go:14`,
`internal/cli/task.go:1244`; тесты `internal/api/tasks_test.go`,
`internal/api/projects_test.go`, `internal/cli/task_test.go`.

- [ ] Тесты: `GET /v1/tasks?board=true` отдаёт ключ `brainstorm` (пустой массив, а не `null`,
      и с задачей внутри, когда она есть); `GET /v1/projects` отдаёт счётчик `brainstorm`;
      `renderTaskBoard` печатает группу `BRAINSTORM` между `BACKLOG` и `IN PROGRESS`.
- [ ] Убедиться, что тесты падают.
- [ ] `Brainstorm []taskResponse `json:"brainstorm"`` между `Backlog` и `InProgress`,
      инициализация в `toTaskBoard`, ветка `case "brainstorm"`;
      `Brainstorm int `json:"brainstorm"`` в `taskCounters`;
      `{"brainstorm", "BRAINSTORM"}` в `statuses`.
- [ ] Тесты зелёные, коммит.

### Задача 5: `brainstorm` — это активная работа (heartbeat и тишина)

**Файлы:** изменить `internal/heartbeat/heartbeat.go:142`, `internal/heartbeat/quiet.go:32,70`,
`internal/api/quiet.go:31`; тесты `internal/heartbeat/heartbeat_test.go`,
`internal/heartbeat/quiet_test.go`.

**Потребляет:** `store.IsActiveTaskStatus` из задачи 1.

**Производит:** `heartbeat.ActiveMilestones(lister) ([]store.Task, error)` — обходит
`store.ActiveTaskStatuses` двумя запросами `ListTasks(TaskFilter{Milestones: true, Status: s})`
и склеивает результат. `TaskFilter` не расширяем.

- [ ] Тесты: оркестратор, чья корневая задача в `brainstorm`, получает heartbeat и
      эскалацию input-stall; майлстон в `brainstorm` признаётся тихим `QuietMilestone` и
      попадает в выборку `sweepQuietMilestones`.
- [ ] Убедиться, что тесты падают.
- [ ] Заменить проверки на `store.IsActiveTaskStatus(task.Status)`, выборки — на
      `ActiveMilestones`.
- [ ] Тесты зелёные, коммит.

### Задача 6: документация

**Файлы:** `docs/12-tasks.md` (список статусов `:20`, таблицы переходов `:36`, `:45`, `:273`,
абзац о пороге тишины `:289`), `docs/05-state.md:136`, `docs/01-concepts.md:87`,
`docs/00-overview.md:33`.

- [ ] Внести `brainstorm` в перечисления и таблицы в каноническом порядке, описать оба
      автоматических перехода, по-русски и в стиле окружающего текста.
- [ ] `make test` целиком зелёный, коммит.
