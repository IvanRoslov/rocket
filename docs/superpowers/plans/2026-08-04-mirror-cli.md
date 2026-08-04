# Свежесть зеркал в CLI (task #795, subtask #927) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rocket status <feature>` и `rocket repo ls` печатают строку свежести по каждому зарегистрированному зеркалу, так что протухшее зеркало нельзя прочитать молча.

**Architecture:** Свежесть — производная величина, в rocket.db она не хранится (docs/05-state.md#свежесть-зеркал), поэтому считает её сам CLI: берёт список репо из `GET /v1/repos` и зовёт `mirror.Check` локально (без сети) по каждому пути. Форматирование живёт в новом `internal/cli/mirror.go` чистыми функциями над `[]mirrorRow`, которые тестируются без единого настоящего git-репозитория — ровно как `renderStatus` тестируется над `[]sessionRow`. Команды `status` и `repo ls` только собирают данные и зовут рендер.

**Tech Stack:** Go, cobra, `internal/mirror`, `text/tabwriter`.

## Global Constraints

- Текст строк заморожен спекой, перефразировать нельзя (docs/04-cli.md:36-52):
  - `mirror rocket: свежее (последний fetch 2 мин назад)`
  - `mirror docs-source: ПРОТУХЛО — рабочее дерево отстаёт на 37 коммитов, последний fetch 3 дня назад`
  - `mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: локальные изменения в зеркале`
  - `mirror broken: свежесть неизвестна (<текст ошибки>)`
- Приоритет причин: `Blocked` → отставание на N коммитов → давность fetch.
- Строки `Blocked` приходят из `internal/mirror` дословно (`BlockedDirty`, `BlockedNoFF`, `BlockedNotOnDefault`), CLI их не переписывает.
- Ошибка `Check` печатает строку «свежесть неизвестна» и **не роняет команду** (exit 0).
- Возраст форматируется существующим `humanAge` (internal/cli/sessions.go:112).
- Никаких сетевых вызовов из CLI; `mirror.Sync` из CLI не зовётся никогда.
- Запрещено трогать `internal/mirror`, `internal/config`, `internal/daemon` — их владеет параллельный воркер.
- Новых зависимостей модуля нет.

---

### Task 1: Рендер строк свежести (`internal/cli/mirror.go`)

**Files:**
- Create: `internal/cli/mirror.go`
- Test: `internal/cli/mirror_test.go`

**Interfaces:**
- Consumes: `mirror.Freshness` (`internal/mirror`), `humanAge(unixSec int64, now time.Time) string`.
- Produces:
  - `type mirrorRow struct { RepoID string; Fresh mirror.Freshness; Err error }`
  - `func mirrorLine(row mirrorRow, now time.Time) string`
  - `func renderMirrors(rows []mirrorRow, w io.Writer, now time.Time)`
  - `const mirrorStaleFallback = 10 * time.Minute`
  - `func mirrorStaleAfter(cfg *config.Config) time.Duration`

- [ ] **Step 1: Написать падающий тест на все четыре случая и приоритет**

```go
func TestMirrorLine(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		row  mirrorRow
		want string
	}{
		{
			name: "fresh",
			row:  mirrorRow{RepoID: "rocket", Fresh: mirror.Freshness{LastFetch: now.Add(-2 * time.Minute)}},
			want: "mirror rocket: свежее (последний fetch 2m назад)",
		},
		{
			name: "behind",
			row: mirrorRow{RepoID: "docs-source", Fresh: mirror.Freshness{
				BehindCommits: 37, LastFetch: now.Add(-72 * time.Hour), Stale: true,
			}},
			want: "mirror docs-source: ПРОТУХЛО — рабочее дерево отстаёт на 37 коммитов, последний fetch 3d назад",
		},
		{
			name: "blocked wins over behind",
			row: mirrorRow{RepoID: "app", Fresh: mirror.Freshness{
				BehindCommits: 5, Blocked: mirror.BlockedDirty, LastFetch: now, Stale: true,
			}},
			want: "mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: локальные изменения в зеркале",
		},
		{
			name: "stale by fetch age only",
			row:  mirrorRow{RepoID: "old", Fresh: mirror.Freshness{LastFetch: now.Add(-3 * time.Hour), Stale: true}},
			want: "mirror old: ПРОТУХЛО — последний fetch 3h назад",
		},
		{
			name: "never fetched",
			row:  mirrorRow{RepoID: "nofetch", Fresh: mirror.Freshness{Stale: true}},
			want: "mirror nofetch: ПРОТУХЛО — fetch ни разу не выполнялся",
		},
		{
			name: "check error",
			row:  mirrorRow{RepoID: "broken", Err: errors.New("not a git repository")},
			want: "mirror broken: свежесть неизвестна (not a git repository)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mirrorLine(tt.row, now); got != tt.want {
				t.Errorf("mirrorLine() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Убедиться, что тест падает**

Run: `go test ./internal/cli/ -run TestMirrorLine`
Expected: FAIL — `undefined: mirrorRow`, `undefined: mirrorLine`.

- [ ] **Step 3: Реализовать `mirrorLine`, `renderMirrors`, `mirrorStaleAfter`**

```go
// mirrorRow is one mirror's freshness as the CLI renders it: either a
// computed Freshness or the error that prevented computing it.
type mirrorRow struct {
	RepoID string
	Fresh  mirror.Freshness
	Err    error
}

// mirrorLine renders one freshness line. The wording is frozen by the spec
// (docs/04-cli.md) — a stale mirror must be impossible to misread.
func mirrorLine(row mirrorRow, now time.Time) string {
	if row.Err != nil {
		return fmt.Sprintf("mirror %s: свежесть неизвестна (%v)", row.RepoID, row.Err)
	}
	if !row.Fresh.Stale {
		return fmt.Sprintf("mirror %s: свежее (%s)", row.RepoID, lastFetchPhrase(row.Fresh.LastFetch, now))
	}
	switch {
	case row.Fresh.Blocked != "":
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — синхронизация не может обновить дерево: %s", row.RepoID, row.Fresh.Blocked)
	case row.Fresh.BehindCommits > 0:
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — рабочее дерево отстаёт на %d %s, %s",
			row.RepoID, row.Fresh.BehindCommits, pluralCommits(row.Fresh.BehindCommits), lastFetchPhrase(row.Fresh.LastFetch, now))
	default:
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — %s", row.RepoID, lastFetchPhrase(row.Fresh.LastFetch, now))
	}
}
```

`lastFetchPhrase` возвращает `"последний fetch <humanAge> назад"`, а для нулевого времени — `"fetch ни разу не выполнялся"`. `pluralCommits` даёт коммит/коммита/коммитов. `renderMirrors` печатает строки по порядку и ничего не печатает на пустом срезе.

- [ ] **Step 4: Убедиться, что тест зелёный**

Run: `go test ./internal/cli/ -run TestMirror`
Expected: PASS

- [ ] **Step 5: Коммит**

```bash
git add internal/cli/mirror.go internal/cli/mirror_test.go
git commit -m "cli: рендер строк свежести зеркал (#795)"
```

---

### Task 2: Сбор свежести из API-списка репо

**Files:**
- Modify: `internal/cli/mirror.go`
- Test: `internal/cli/mirror_test.go`

**Interfaces:**
- Produces:
  - `type repoRow struct { ID, Path, DefaultBranch string }` с json-тегами `id`, `path`, `default_branch`
  - `func checkMirrors(ctx context.Context, repos []repoRow, staleAfter time.Duration, now time.Time) []mirrorRow`

- [ ] **Step 1: Написать падающий тест**

Тест строит два `repoRow` с заведомо несуществующими путями и проверяет, что `checkMirrors` вернёт по строке на каждый, в том же порядке, с непустым `Err` — то есть что ошибка одного зеркала не отменяет остальных и не паникует.

- [ ] **Step 2: Убедиться, что тест падает** — `undefined: checkMirrors`.

- [ ] **Step 3: Реализовать `checkMirrors`**: на каждый репо `mirror.Check(ctx, store.Repo{ID, Path, DefaultBranch}, staleAfter, now)`, ошибка кладётся в `mirrorRow.Err`, а не возвращается наружу. Общий таймаут ограничивается вызывающей стороной через `ctx`.

- [ ] **Step 4: Тест зелёный.**

- [ ] **Step 5: Коммит** — `cli: checkMirrors — свежесть по списку репо (#795)`.

---

### Task 3: `rocket repo ls`

**Files:**
- Modify: `internal/cli/repo.go:66-99`
- Test: `internal/cli/repo_test.go`

- [ ] **Step 1: Тест на рендер таблицы + блока зеркал** (чистая функция рендера, без обращения к демону).
- [ ] **Step 2: Убедиться, что падает.**
- [ ] **Step 3: Реализация:** `repo ls` декодирует ответ в `[]repoRow`, печатает существующую таблицу ID/PATH/BRANCH, затем `renderMirrors`. В `--json` каждый объект получает дополнительное поле `mirror` со структурой свежести (`behind_commits`, `last_fetch`, `blocked`, `stale`, `error`) — машинный вывод не должен молчать о том, о чём говорит человеческий.
- [ ] **Step 4: Тест зелёный.**
- [ ] **Step 5: Коммит.**

---

### Task 4: `rocket status <feature>`

**Files:**
- Modify: `internal/cli/status.go`
- Test: `internal/cli/status_test.go`

- [ ] **Step 1: Тест:** `renderStatus` c зеркалами печатает блок после таблицы воркеров, и сессии рендерятся даже когда у зеркала `Err`.
- [ ] **Step 2: Убедиться, что падает.**
- [ ] **Step 3: Реализация:** `renderStatus(slug, sessions, mirrors, w, now)` — блок зеркал после таблицы, отделён пустой строкой. `RunE` собирает `GET /v1/repos` и зовёт `checkMirrors`; ошибка самого запроса `/v1/repos` не роняет команду.
- [ ] **Step 4: Тест зелёный.**
- [ ] **Step 5: Коммит.**

---

### Task 5: Подключить `cfg.MirrorSyncInterval`

Ждёт мержа параллельной задачи mirror-daemon (поля в `internal/config` пока нет, трогать его нельзя).

- [ ] **Step 1:** Заменить тело `mirrorStaleAfter` на `2 * cfg.MirrorSyncInterval`, оставив фолбэк 10m при нуле.
- [ ] **Step 2:** `go build ./... && go test ./internal/cli/...`
- [ ] **Step 3:** Коммит в этот же PR.

---

### Verify (перед объявлением готовности)

- `go test ./internal/cli/...` — зелено, кроме заранее известного `TestLoadConfigNoOverrideWhenSocketFlagEmpty` (чужой, задача #924).
- `go build ./... && go vet ./... && gofmt -l internal/cli` — чисто.
- Живой прогон собранного бинаря: `rocket repo ls` и `rocket status <slug>`, вывод — в отчёт оркестратору.
