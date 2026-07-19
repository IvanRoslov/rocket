# Rocket Mobile — прототип мобильного клиента (Expo / React Native)

Дата: 2026-07-19. Статус: прототип. Дизайн: claude.ai/design «Модульная структура дашборда», файлы `*.mobile.dc.html`.

## Цель

Смотреть и слегка управлять rocket-демоном с телефона в локальной сети: видеть проекты/задачи/сессии, отвечать на вопросы оркестраторов, писать им сообщения, подглядывать в терминалы. Демонов два (два компьютера) — приложение умеет хранить несколько серверов и переключаться.

## Не-цели (v1)

- Мастер создания проекта (NewProject) — делается с десктопа.
- Интерактивный терминал (WebSocket-ввод), SSE-стрим событий — только поллинг.
- Аутентификация — API демона без auth, доверенная домашняя сеть.
- Kill/restore сессий, cleanup системы — только просмотр.

## Изменение демона (обязательная предпосылка)

`internal/config`: новое поле `host` (yaml `host`, default `127.0.0.1`).
`internal/api/server.go`: биндить `net.Listen("tcp", host:port)` вместо жёсткого `127.0.0.1`.
Для телефона пользователь ставит `host: 0.0.0.0` в `~/.rocket/config.yaml`.

## Архитектура приложения

Каталог `mobile/` в корне репозитория. Expo SDK (managed), TypeScript strict, expo-router (file-based навигация), @tanstack/react-query (поллинг), @react-native-async-storage/async-storage (серверы). Без нативных модулей — работает в Expo Go.

```
mobile/
  app/
    _layout.tsx          # QueryClientProvider + ServerProvider + Stack
    servers.tsx          # стартовый экран выбора/добавления сервера
    (tabs)/_layout.tsx   # нижний таб-бар: Projects / Kanban / System / Settings
    (tabs)/index.tsx     # Projects
    (tabs)/kanban.tsx    # Kanban активного проекта (фильтр-чипы по колонкам)
    (tabs)/system.tsx    # System
    (tabs)/settings.tsx  # Settings (GitHub/Repos/Daemon, read-only + токен)
    task/[id].tsx        # Задача: Questions/Overview/Docs/Journal/Messages
    term/[id].tsx        # Read-only терминал сессии (поллинг output)
  src/
    api/client.ts        # fetch-обёртка: baseUrl активного сервера, ошибки {error:{code,message}}
    api/queries.ts       # useProjects/useTasks/useTaskDetail/useSystem/... (react-query)
    api/types.ts         # порт web/src/lib/types.ts (типы демона)
    servers/ServerContext.tsx  # список серверов + активный, персист в AsyncStorage
    theme.ts             # токены из дизайна (цвета/радиусы/типографика)
    components/          # Badge, Dot, Card, Chip, SectionTitle, ...
  app.json, package.json, tsconfig.json
```

## Серверы

Модель: `{id, name, host, port}`. Экран Servers: список карточек с живым статусом (GET `/v1/health`, версия/uptime или «offline»), кнопка «＋ Add server» (имя, host, port; default 4477), тап — сделать активным и перейти в Projects, long-press/кнопка — удалить. Активный сервер показан в хедере Projects (chip с точкой статуса); тап по нему — назад в Servers. Если серверов нет — приложение стартует на Servers. Ошибки сети на любом экране показывают баннер «server unreachable» с кнопкой «Switch server».

## Данные

- Поллинг: projects/tasks/system — 5 с; карточка задачи и вопросы — 3 с; терминал — 2 с.
- Ключи query включают id сервера — переключение сервера инвалидирует кэш.
- Мутации: PATCH task status (перетаскивания нет — из карточки), POST `/v1/tasks`, POST `/v1/tasks/{id}/start`, POST `/v1/questions/{id}/reply|answer`, POST `/v1/messages`, PUT `/v1/settings`.

## Экраны (по дизайну)

- **Projects** — карточки: точка статуса, имя, id, main + linked-репо, чипы-агрегаты (in progress/review/live), «? awaiting you», «⚠ problem», updated. Тап — Kanban проекта. Данные: GET `/v1/projects` + GET `/v1/tasks?board=true` (агрегаты по проектам) — если агрегатов в API нет, считаем на клиенте из /v1/tasks и /v1/sessions.
- **Kanban** — чипы-колонки (Backlog/In Progress/Review/Done) с каунтами, поиск, карточки задач (репо, orch-статус, PR-бейджи, сигналы), «Start orchestrator ▸» у backlog, FAB «＋» — модалка создания задачи (title/description). Селектор проекта в хедере.
- **Task** — хедер (#id, title, статус-бейдж), баннер «? awaiting», вкладки: Questions (тред, Clarify/Answer & close, resolved-список), Overview (описание, подзадачи, final report), Docs, Journal (таймлайн), Messages (чат с оркестратором + отправка). Нижняя кнопка «Sessions · N» — шторка: оркестратор + воркеры, «Open terminal», «attach ⧉» (копия команды в буфер).
- **Terminal** — тёмный фон, моноширинный текст, поллинг `output?lines=200`, ANSI-стрип, автоскролл вниз.
- **System** — стат-карточки (live sessions/agents/orphans/queue), список сессий, очередь (queued/failed), worktrees с размерами, карточка демона, хвост лога.
- **Settings** — чипы-секции: GitHub (токен, сохранить, «Authorized as»), Repositories (read-only список), Daemon (read-only параметры + активный сервер).

## Тема

Светлая, из дизайна: фон `#f6f6f4`, карточки `#fff`, бордер `#e7e7e4`, текст `#1a1a1c`, вторичный `#71717a`/`#a1a1aa`, акцент `#4f46e5`, статусы: green `#16a34a`, amber `#d97706/#b45309`, red `#dc2626/#b91c1c`, фиолетовый review `#7c3aed`. Радиусы 9–14, чипы 20. Терминал: `#161618`.

## Ошибки

Единый формат демона `{error:{code,message}}` → toast/inline. Недоступный сервер → полноэкранное состояние с retry и переходом на Servers.

## Тестирование

Прототип: typecheck (`tsc --noEmit`) + ручная проверка против живого демона. Go-изменение (`host`) — юнит-тест на дефолт и парсинг yaml.
