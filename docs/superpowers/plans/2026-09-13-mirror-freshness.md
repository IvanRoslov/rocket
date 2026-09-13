# Свежесть зеркал ~/.rocket/repos — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development для каждой задачи,
> superpowers:verification-before-completion перед созданием PR. Шаги отмечаются `- [ ]`.

**Goal:** зеркала под `~/.rocket/repos` наблюдаемы и чинятся командами `rocket repo status` /
`rocket repo sync [--repair]`, подтягиваются синхронно перед стартом сессии, а несинхронизированное
зеркало видно в `rocket task show`.

**Architecture:** переиспользуем существующий `internal/mirror` (`Sync`, `Check`, `Freshness`).
Добавляем в него функцию ремонта, в `internal/cli/repo.go` — две подкоманды, в `internal/session` —
вызов `mirror.Sync` перед `ws.Create`, в `internal/cli/task.go` — строку свежести. Зеркала остаются
обычными клонами с рабочим деревом; bare отвергнут.

**Tech Stack:** Go, cobra, `go test ./...`, `make`.

**Spec:** `docs/superpowers/specs/2026-09-13-mirror-freshness-design.md`

## Global Constraints

- Никаких `git reset --hard`, `checkout -f`, `clean` в коде — инвариант пакета `mirror` (см. шапку `internal/mirror/mirror.go`).
- Зеркалом считается только репозиторий под `cfg.ReposDir`; рабочие копии из `repo add <path>` не трогаем.
- Русский язык в пользовательском выводе CLI, английский в коде, идентификаторах и коммитах.
- `--json` может только добавлять ключи, никогда не двигать существующие.
- Все новые команды работают без сети, кроме `repo sync`, который явно ходит в origin.

---

### Task 1: `mirror.Repair` — ремонт заблокированного зеркала

**Files:**
- Modify: `internal/mirror/mirror.go`
- Test: `internal/mirror/mirror_test.go`

**Interfaces:**
- Produces: `func Repair(ctx context.Context, repo store.Repo, now time.Time) (RepairResult, error)`,
  `type RepairResult struct { RescueBranch string; Repaired bool; Blocked string }`.

- [ ] **Шаг 1.** Написать падающие тесты в `mirror_test.go` (существующие хелперы создания временного
      репозитория в файле переиспользовать): (a) грязное зеркало → правки уезжают коммитом в ветку
      `rescue/<timestamp>`, рабочее дерево чистое, `RescueBranch` непустой; (b) зеркало на чужой ветке
      → HEAD возвращается на default-ветку, чужая ветка не удаляется; (c) чистое актуальное зеркало →
      `Repaired=false`, ничего не изменилось; (d) невозможный FF после перевода HEAD → `Blocked=BlockedNoFF`.
- [ ] **Шаг 2.** `go test ./internal/mirror/ -run Repair -v` — убедиться, что падает.
- [ ] **Шаг 3.** Реализовать `Repair`: при грязном дереве `git checkout -b rescue/<now.Format("2006-01-02-150405")>`,
      `git add -A`, `git commit -m "rescue: uncommitted mirror changes"`; затем `git checkout <default>`;
      затем вызвать `Sync`. Имя ветки при коллизии — с суффиксом `-2`, `-3`, …
- [ ] **Шаг 4.** `go test ./internal/mirror/ -v` — зелено.
- [ ] **Шаг 5.** Коммит: `feat(mirror): add Repair for blocked mirrors`.

---

### Task 2: `rocket repo status` и `rocket repo sync [--repair]`

**Files:**
- Modify: `internal/cli/repo.go` (группа команд, алиас `repos`)
- Create: `internal/cli/repo_sync.go`, `internal/cli/repo_status.go`
- Test: `internal/cli/repo_sync_test.go`, `internal/cli/repo_status_test.go`

**Interfaces:**
- Consumes: `mirror.Check`, `mirror.Sync`, `mirror.Repair` (Task 1), `mirrorsOnly` из `internal/cli/mirror.go`.

- [ ] **Шаг 1.** Добавить группе `repo` `Aliases: []string{"repos"}`; тест, что `rocket repos ls` резолвится.
- [ ] **Шаг 2.** Падающие тесты рендера таблицы `repo status` на фикстурах `mirror.Freshness` +
      дополнительных полей (head/origin SHA, ветка, dirty): колонки `repo head origin behind branch dirty fetched`,
      и `--json` с теми же полями.
- [ ] **Шаг 3.** Реализовать `repo status`: расширить сбор данных (короткие SHA и имя ветки берём
      локальным git, как это делает `mirror.Check`), отрендерить таблицу и JSON.
- [ ] **Шаг 4.** Падающие тесты рендера итогов `repo sync`: строки «обновлено на N коммитов»,
      «уже актуально», «заблокировано: <причина>»; код возврата 0 при частичных блокировках,
      ненулевой — только если ни одно зеркало не обработано.
- [ ] **Шаг 5.** Реализовать `repo sync [<id>…]` и флаг `--repair`, который для заблокированных зеркал
      вызывает `mirror.Repair` и печатает отчёт: починено / спасательная ветка / осталось заблокированным.
- [ ] **Шаг 6.** `go test ./internal/cli/ -v` — зелено.
- [ ] **Шаг 7.** Коммит: `feat(cli): add repo status and repo sync --repair`.

---

### Task 3: синхронная подтяжка зеркала перед стартом сессии + строка в `rocket task show`

**Files:**
- Modify: `internal/session/manager.go:250` (перед `ws.Create`), `internal/cli/task.go` (`newTaskShowCmd`, `renderTaskCard`)
- Test: `internal/session/manager_test.go`, `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `mirror.Sync`, `mirror.Check`, `mirrorFreshness`/`renderMirrorLine` из `internal/cli/mirror.go`.

- [ ] **Шаг 1.** Падающий тест в `manager_test.go`: спавн при зеркале, которое нельзя подтянуть
      (грязное), — сессия всё равно создаётся, ошибка не возвращается.
- [ ] **Шаг 2.** Падающий тест: спавн для репозитория вне `repos_dir` не вызывает `mirror.Sync`.
- [ ] **Шаг 3.** Реализовать вызов `mirror.Sync` перед `ws.Create` под отдельным таймаутом
      (константа в `manager.go`, 60s), результат — только `slog.Warn`, не ошибка.
- [ ] **Шаг 4.** Падающий тест рендера `task show`: при протухшем зеркале карточка содержит строку
      `mirror <id>: ПРОТУХЛО — …`, при свежем — не содержит; `--json` получает ключ `mirrors`.
- [ ] **Шаг 5.** Реализовать: в `newTaskShowCmd` посчитать свежесть зеркал локально и передать в
      `renderTaskCard`; добавить поле `Mirrors` в `taskShowJSON` последним ключом.
- [ ] **Шаг 6.** `go test ./internal/session/ ./internal/cli/ -v` — зелено.
- [ ] **Шаг 7.** Коммит: `feat(session,cli): sync mirror before spawn and surface staleness in task show`.

---

### Task 4: интеграционный тест свежести воркспейса + документация

**Files:**
- Create: `internal/workspace/freshness_integration_test.go`
- Modify: `docs/04-cli.md`, `docs/05-state.md`, `docs/02-architecture.md`

- [ ] **Шаг 1.** Падающий интеграционный тест: создать локальный «origin» (bare репозиторий),
      клонировать его как зеркало, сделать новый коммит в origin, затем `mirror.Sync` + `workspace.Create`
      и проверить, что новый воркспейс содержит этот коммит без ручного `pull`.
- [ ] **Шаг 2.** `go test ./internal/workspace/ -v` — убедиться, что тест осмысленно падает, если
      убрать `mirror.Sync` из сценария.
- [ ] **Шаг 3.** Довести тест до зелёного.
- [ ] **Шаг 4.** `docs/04-cli.md`: раздел про `rocket repo status`, `rocket repo sync`, `--repair`, алиас `repos`,
      с примерами вывода.
- [ ] **Шаг 5.** `docs/05-state.md`: инвариант «зеркало всегда на default-ветке», спасательные ветки `rescue/*`,
      что именно делает `--repair` и чего он никогда не делает.
- [ ] **Шаг 6.** `docs/02-architecture.md`: синхронная подтяжка зеркала перед созданием воркспейса и её
      неблокирующий характер.
- [ ] **Шаг 7.** Коммит: `test,docs: workspace freshness integration test and mirror docs`.

---

## Порядок и параллельность

- Task 1 — первым, от него зависит Task 2.
- Task 3 независим от 1 и 2 — можно параллельно с Task 1.
- Task 4 стартует после Task 2 (документирует итоговые команды).
