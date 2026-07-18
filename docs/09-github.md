# Интеграция с GitHub

## Аутентификация

Пользователь один раз вводит GitHub-токен (PAT, scopes `repo`) — в дашборде (Settings) или `rocket github auth <token>`. Токен хранится в таблице `settings` базы демона (файл 0600; этого уровня защиты достаточно для локального инструмента). Демон работает с GitHub REST API напрямую с этим токеном — `gh` CLI демону не нужен.

Агенты в сессиях продолжают пользоваться `gh` (например `gh pr create`); демон прокидывает токен в env сессий (`GH_TOKEN`), так что отдельная авторизация агентам не требуется.

## Каталог репозиториев

Для UI добавления репо:

- `GET /v1/github/repos?q=<поиск>` — демон отдаёт список доступных токену репозиториев (owner/name, private, default_branch), с кэшем на несколько минут.
- `POST /v1/repos {github: "owner/name"}` — rocket сам клонирует репо в `~/.rocket/repos/<name>` и регистрирует его в реестре. Альтернатива — как раньше, `{path: ...}` для уже существующего локального чекаута.

Демон ничего не пишет в GitHub — только читает; PR открывают сами агенты.

## Привязка PR к сессии

Источник правды — ветка: для воркера `feature/<slug>/<task>` демон ищет PR по head-ветке через REST API (`GET /repos/{owner}/{repo}/pulls?head=...`). `owner/repo` выводится из `git remote origin` репозитория. Найденный PR пишется в `sessions.pr_number`. Отдельного hook-парсинга `gh pr create` (как metadata-updater в AO) не требуется — поллинг по ветке проще и не зависит от агента.

## Поллер

Цикл каждые 2m (конфигурируемо) по всем живым воркерам:

1. Нет `pr_number` → поиск PR по ветке; найден → событие `pr.opened`.
2. Есть PR → REST API (pull + check-runs + reviews):
   - `ci_state`: rollup чеков → `pending|passing|failing`; смена → событие `pr.ci_changed`;
   - `pr_state`: `open|merged|closed`.
3. Rate-limit дружелюбие: батчинг по репозиториям, conditional requests (ETag), backoff при ошибках API.

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
