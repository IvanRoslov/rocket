# CLI

Один бинарник `rocket`. Все команды — клиенты API демона; при недоступном сокете демон автозапускается (кроме `daemon *`). Глобальные флаги: `--json` (машинный вывод), `--socket <path>`.

Внутри rocket-сессий выставлены env: `ROCKET_SESSION_ID`, `ROCKET_KIND`, `ROCKET_PARENT_ID`, `ROCKET_PROJECT_ID`, `ROCKET_FEATURE`. CLI использует их для автоопределения вызывающего.

## Для пользователя

```
rocket up "<описание фичи>" [--project <id>] [--agent <name>]
    Запустить оркестратор под фичу. Проект по умолчанию — определяется по cwd,
    иначе обязателен. Печатает feature slug и session id.

rocket ls [--project <id>] [--feature <slug>] [--all]
    Таблица сессий: id, kind, project, activity, PR/CI, возраст.
    По умолчанию только живые; --all включает терминальные.

rocket status <feature-slug>
    Сводка фичи: оркестратор + его воркеры со статусами, PR, CI.

rocket attach <session>
    exec tmux attach к сессии.

rocket send <session> "<текст>" | --file <path>
    Положить сообщение в очередь. Возвращается сразу; --wait — дождаться доставки.

rocket kill <session> [--cleanup]
    Убить сессию. У оркестратора --cascade убивает и его воркеров.

rocket restore <session>
    Восстановить упавшую сессию (после ребута и т.п.).

rocket project add <path> [--id <id>]
rocket project ls
rocket project link <hub> <target>      # добавить target в links хаба
rocket project unlink <hub> <target>
rocket project rm <id>

rocket events [--follow] [--session <id>]
rocket logs [--follow]                  # логи самого демона
rocket daemon start|stop|status|run     # run — foreground (для отладки/launchd)
rocket doctor                           # проверка окружения: tmux, git, gh, агенты
```

## Для агентов (вызываются оркестратором/воркером из своей сессии)

```
rocket spawn --task <task> --project <id> --prompt "<бриф>" [--agent <name>]
    Только для оркестраторов. Спавнит воркера <feature>-<task> на ветке
    feature/<feature>/<task> в указанном проекте. Печатает session id.

rocket send <session> "<текст>"
    То же, что у пользователя; from заполняется из ROCKET_SESSION_ID,
    получателю текст приходит с префиксом "[from <id>] ".

rocket ls / rocket status <slug>
    Оркестратор проверяет своих воркеров.

rocket kill <session>
    Оркестратор может убивать только своих воркеров.
```

## Коды выхода

`0` успех; `1` ошибка API/валидации; `2` демон недоступен и не смог запуститься; `3` неверное использование команды.
