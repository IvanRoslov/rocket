# CLI

Один бинарник `rocket`. Все команды — клиенты API демона; при недоступном сокете демон автозапускается (кроме `daemon *`). Глобальные флаги: `--json` (машинный вывод), `--socket <path>`.

Внутри rocket-сессий выставлены env: `ROCKET_SESSION_ID`, `ROCKET_KIND`, `ROCKET_PARENT_ID`, `ROCKET_PROJECT_ID`, `ROCKET_REPO_ID`, `ROCKET_FEATURE`. CLI использует их для автоопределения вызывающего.

## Для пользователя

```
rocket task add "<title>" [--project <id>] [--desc <md>|--desc-file <f>]
rocket task ls [--status <s>] [--project <id>]   # канбан в терминале
rocket task show <id>       # карточка: подзадачи, доки, журнал, attach-команда
rocket task start <id> [--agent <name>]          # назначить оркестратора
rocket task move <id> <status>
rocket task doc put <id> --kind spec|plan|report --title "..." --file <f.md>
rocket task log <id> --kind decision|problem|note "<текст>"
rocket task questions [<id>] [--open]            # вопросы и их треды
rocket task reply <question-id> "<уточнение>"    # реплика в тред, вопрос открыт
rocket task answer <question-id> "<ответ>"       # финальный ответ, закрывает
rocket task answer <question-id> --dismiss       # закрыть как неактуальный
rocket task cancel <id>
    Подробности — 12-tasks.md.

rocket up "<описание фичи>" [--project <id>] [--agent <name>]
    Шорткат: task add + task start. Проект по умолчанию — по cwd.
    Печатает task id, feature slug и session id.

rocket ls [--project <id>] [--feature <slug>] [--all]
    Таблица сессий: id, kind, project, activity, PR/CI, возраст.
    По умолчанию только живые; --all включает терминальные.

rocket status <feature-slug>
    Сводка фичи: оркестратор + его воркеры со статусами, PR, CI.

rocket verify-merge <subtask-id | worker-session-id>
    Контент-проверка мержа PR подзадачи: сравнивает origin/<default-branch>
    с origin/<веткой воркера> ТОЛЬКО по remote-ref'ам (сначала fetch), так
    что результат не зависит от cwd, протухшего чекаута и незакоммиченных
    правок. Пустой диф — содержимое ветки полностью в default-ветке;
    непустой сопровождается дифом собственных изменений ветки (three-dot)
    и инструкцией, как читать пересечение (после мержа в default могли
    уехать чужие PR). Ветка удалена на origin (сквош-мерж) — отдельное
    сообщение с ручным фолбэком.

rocket attach <session|task-id>
    exec tmux attach (изнутри tmux — switch-client). Числовой аргумент
    трактуется как id задачи и резолвится в её сессию.

rocket send <session> "<текст>" | --file <path>
    Положить сообщение в очередь. Возвращается сразу; --wait — дождаться доставки.

rocket kill <session> [--cleanup]
    Убить сессию. У оркестратора --cascade убивает и его воркеров.

rocket restore <session>
    Восстановить упавшую сессию (после ребута и т.п.).

rocket repo add <path> [--id <id>]      # зарегистрировать локальный чекаут
rocket repo add --github <owner/name>   # склонировать в ~/.rocket/repos и зарегистрировать
rocket repo ls / rocket repo rm <id>

rocket github auth <token>              # сохранить GitHub-токен (то же, что Settings в UI)

rocket project create <id> --main <repo> [--link <repo>]... [--name "..."]
rocket project ls                       # проекты с агрегатами (задачи, сессии)
rocket project show <id>
rocket project link <project> <repo>    # добавить linked-репо
rocket project unlink <project> <repo>
rocket project rm <id>

rocket events [--follow] [--session <id>]
rocket logs [--follow]                  # логи самого демона
rocket daemon start|stop|status|run     # run — foreground (для отладки/launchd)
rocket doctor                           # проверка окружения: tmux, git, gh, агенты
```

### Постоянные агенты

```
rocket agent add <id> [--description "..."] [--project <p>]
                      [--dir <path>] [--command "<cmd>"]
    Регистрирует агента. id — [a-z0-9-], он же имя его tmux-сессии.
    dir/command нужны только для rocket agent start.
rocket agent edit <id> [--description|--project|--dir|--command ...]
rocket agent ls [--project <p>]   # id, проект, enabled, живость сессии,
                                  # непрочитанные, открытые треды, описание
rocket agent show <id>            # регистрация + непрочитанные сообщения
rocket agent enable|disable <id>
rocket agent rm <id>              # из реестра; файлы агента остаются на диске

rocket agent start <id>           # tmux-сессия <id>: cwd=dir, команда command
                                  # (нет command — shell; нет dir — ошибка)
rocket agent attach <id>          # подключиться к сессии агента
rocket agent stop <id>            # убить сессию; регистрация остаётся

rocket agent ask <id> "<вопрос>" [--context <md>]
    Открыть тред-вопрос агенту. Изнутри сессии агента тот же вызов
    открывает тред человеку.
rocket agent questions [<id>] [--open]
    Треды агента (без аргумента — агент текущей сессии).
rocket agent reply <qid> "<текст>"      # ответ в тред (обе стороны)
rocket agent answer <qid> "<ответ>" | --dismiss
    Закрыть тред; только человек. Агент может оспорить закрытый тред
    своим reply — тред переоткроется.
```

### Инбокс агента (изнутри его сессии)

```
rocket inbox [--agent <id>]       # счётчик + непрочитанные: id, from, возраст,
                                  # первая строка
rocket inbox next [--agent <id>]  # самое старое непрочитанное целиком,
                                  # помечается прочитанным
rocket inbox peek <msg-id>        # прочитать конкретное, не помечая
```

Id агента берётся из `--agent`, иначе из `ROCKET_SESSION_ID`, иначе из имени
tmux-сессии — команды работают и в сессии, поднятой вручную.

## Для агентов (вызываются оркестратором/воркером из своей сессии)

```
rocket spawn --task <name> --repo <id> --prompt "<бриф>" [--agent <name>]
             [--subtask <id>]
    Только для оркестраторов. Спавнит воркера <feature>-<name> на ветке
    feature/<feature>/<name> в указанном репозитории (main или linked
    репо проекта оркестратора). Автоматически создаёт
    подзадачу (in_progress) и привязывает к воркеру; --subtask привязывает
    к заранее созданной подзадаче. Печатает session id и subtask id.

rocket task ... (см. выше)
    Оркестратор ведёт доки/журнал своей задачи, воркер — своей подзадачи.

rocket task ask <id> "<вопрос>" [--context <md>]
    Только для оркестраторов: открыть вопрос пользователю через задачу.
    Пользователь увидит его в дашборде/CLI. Вопрос — тред: уточнения приходят
    как "[task #N QM reply] ..." (отвечать rocket task reply в тот же тред),
    финальный ответ — "[task #N QM answer] ...".

rocket send <session|role> "<текст>"
    То же, что у пользователя; from заполняется из ROCKET_SESSION_ID,
    получателю текст приходит с префиксом "[from <id>] ".
    Если адресат — не сессия, а роль (см. rocket agent), сообщение ложится
    в её инбокс и будит её; --wait в этом случае неприменим.

rocket ls / rocket status <slug>
    Оркестратор проверяет своих воркеров.

rocket kill <session>
    Оркестратор может убивать только своих воркеров.

rocket agent state set <kind>:<ref> <state> [--note "..."] [--until 2026-08-15]
                       [--task <id>] [--agent <role>]
rocket agent state ls [--state deferred] [--agent <role>]
    Досье роли (kind: issue|task|ping). Роль определяется по ROCKET_SESSION_ID
    инстанса (<role>-run-<n>); вне инстанса нужен --agent.

rocket agent ask <role> "<вопрос>" / rocket agent reply <qid> "<текст>"
    Q&A-треды роли. Изнутри инстанса ask эскалирует вопрос человеку,
    reply отвечает в тред, открытый человеком (сообщения приходят
    как "[role <id> Q<n> question|reply|answer] ...").
```

## Коды выхода

`0` успех; `1` ошибка API/валидации; `2` демон недоступен и не смог запуститься; `3` неверное использование команды.
