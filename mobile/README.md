# Rocket Mobile

Мобильный клиент rocket-демона (Expo / React Native). Смотрит и управляет проектами, задачами и агентскими сессиями с телефона в локальной сети; поддерживает несколько серверов (несколько компьютеров с rocketd).

## Запуск

1. На компьютере с демоном разреши доступ по LAN — в `~/.rocket/config.yaml`:

   ```yaml
   host: 0.0.0.0   # по умолчанию 127.0.0.1 (только localhost)
   ```

   и перезапусти rocketd (бинарник должен включать коммит `1d8b8c0` или новее).

2. Запусти dev-сервер и открой в Expo Go (телефон в той же сети):

   ```sh
   cd mobile
   npm install
   npx expo start
   ```

3. На стартовом экране добавь сервер: имя, IP компьютера (`ipconfig getifaddr en0`), порт (по умолчанию 4477). Серверов может быть несколько — переключение по чипу в хедере Projects или в Settings → Server.

## Скрипты

| Команда | Что делает |
|---|---|
| `npm test` | jest-тесты (jest-expo + RNTL) |
| `npx tsc --noEmit` | typecheck |
| `npm run gen:terminal` | перегенерировать `src/terminal/terminalHtml.ts` после апгрейда `@xterm/*` |
| `npx expo export` | production-бандл |

## Архитектура

```
app/                    # expo-router
  servers.tsx           # стартовый экран выбора/добавления сервера
  (tabs)/               # Projects / Kanban / System / Settings
  task/[id].tsx         # задача: Questions/Overview/Docs/Journal/Messages + шторка сессий
  term/[id].tsx         # интерактивный терминал (WebView + xterm.js + WS), fallback на снапшоты
  project/new.tsx       # мастер создания проекта
  project/[id]/settings.tsx
src/
  api/client.ts         # fetch-обёртка, ошибки {error:{code,message}} → ApiError
  api/queries.ts        # все хуки запросов/мутаций (TanStack Query)
  api/events.ts         # SSE /v1/events/stream → инвалидация кэша; статус соединения
  api/types.ts          # типы API демона (порт web/src/lib/types.ts)
  servers/ServerContext.tsx  # список серверов + активный (AsyncStorage)
  terminal/protocol.ts  # ws-URL и спец-клавиши; terminalHtml.ts — сгенерированная страница xterm
  components/           # ui-kit по токенам дизайна (theme.ts)
```

Данные: SSE-события демона инвалидируют кэш TanStack Query (поллинг остаётся медленным safety-net'ом; при обрыве стрима — быстрый поллинг и баннер под хедером). Терминал: WebView владеет собственным WebSocket на `/v1/sessions/{id}/term` (бинарные фреймы — байты, текстовые — resize/ping), тулбар спец-клавиш для мобильной клавиатуры.

Дизайн — мобильные макеты из claude.ai/design проекта «Модульная структура дашборда» (`*.mobile.dc.html`); токены в `src/theme.ts`.
