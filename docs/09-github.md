# Интеграция с GitHub

Вся работа с GitHub — через `gh` CLI (авторизация — забота пользователя, `rocket doctor` проверяет). Демон ничего не пишет в GitHub — только читает; PR открывают сами агенты через `gh pr create`.

## Привязка PR к сессии

Источник правды — ветка: для воркера `feature/<slug>/<task>` демон ищет PR по head-ветке:

```
gh pr list --repo <repo> --head feature/<slug>/<task> --json number,state,...
```

`repo` выводится из `git remote origin` проекта. Найденный PR пишется в `sessions.pr_number`. Отдельного hook-парсинга `gh pr create` (как metadata-updater в AO) не требуется — поллинг по ветке проще и не зависит от агента.

## Поллер

Цикл каждые 2m (конфигурируемо) по всем живым воркерам:

1. Нет `pr_number` → поиск PR по ветке; найден → событие `pr.opened`.
2. Есть PR → `gh pr view <n> --json state,statusCheckRollup,reviewDecision`:
   - `ci_state`: rollup чеков → `pending|passing|failing`; смена → событие `pr.ci_changed`;
   - `pr_state`: `open|merged|closed`.
3. Rate-limit дружелюбие: батчинг по репозиториям, backoff при ошибках `gh`.

## Реакции

Минимальный набор (v1), всё через очередь сообщений:

- **CI упал** (`passing|pending → failing`): сообщение воркеру
  `[rocket] CI failing on PR #<n>: <краткая сводка упавших чеков>. Investigate and fix.`
  Не чаще раза на SHA.
- **Changes requested** (reviewDecision): сообщение воркеру с просьбой обработать ревью.
- Эскалации оркестратору не дублируются — heartbeat и так включает CI-статусы воркеров.

## Авто-cleanup после merge

`pr_state → merged` у воркера:

1. Grace-период 5m (воркер мог продолжать пост-merge действия).
2. Если после grace воркер не `active`: kill tmux, destroy worktree (ветка остаётся), `state=done`, событие `workspace.cleanup`.
3. Подзадача воркера автоматически переводится: PR открыт → `review`, PR смержен → `done` (запись в `task_log`).
3. Оркестраторов авто-cleanup не касается никогда.

Отключается per-project (`auto_cleanup: false` в конфиге проекта).
