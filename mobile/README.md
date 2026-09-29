# Rocket Mobile

Мобильный клиент rocket-демона (Expo / React Native). Смотрит и управляет проектами, задачами и агентскими сессиями с телефона по Tailscale; поддерживает несколько серверов (несколько компьютеров с rocketd).

## Запуск

1. На компьютере с демоном настрой доступ через Tailscale (полностью — `docs/testing/remote-access.md`): поставь Tailscale на Mac и телефон, выполни `tailscale serve --bg 4477`, в `~/.rocket/config.yaml` укажи `host: 127.0.0.1` и `public_url: https://<mac>.<tailnet>.ts.net`, перезапусти rocketd (`make restart`). Демон по TCP всегда требует токен устройства.

2. Запусти dev-сервер и открой в Expo Go (телефон в той же сети):

   ```sh
   cd mobile
   npm install
   npx expo start
   ```

3. На компьютере выполни `rocket pair` — он покажет QR и одноразовый код. В приложении на экране серверов нажми «Сканировать QR» (или введи адрес и код вручную); ссылка `rocketmobile://pair?...` тоже открывает подтверждение. Токен устройства хранится в SecureStore. Серверов может быть несколько — переключение по чипу в хедере Projects или в Settings → Server. Если доступ отозван (`rocket devices revoke`), появится баннер «подключи заново».

## Скрипты

| Команда | Что делает |
|---|---|
| `npm test` | jest-тесты (jest-expo + RNTL) |
| `npx tsc --noEmit` | typecheck |
| `npx expo export` | production-бандл |

## Архитектура

```
app/                    # expo-router
  servers.tsx           # выбор сервера + сопряжение (QR / адрес и код)
  pair.tsx              # deep link rocketmobile://pair?url&code — подтверждение
  (tabs)/               # Projects / Kanban / System / Settings
  task/[id].tsx         # задача: Questions/Overview/Docs/Journal/Messages + шторка сессий
  chat/[id].tsx         # чат с сессией: транскрипт агента + композер (docs/13-chat.md)
  project/new.tsx       # мастер создания проекта
  project/[id]/settings.tsx
src/
  api/client.ts         # fetch-обёртка, ошибки {error:{code,message}} → ApiError
  api/queries.ts        # все хуки запросов/мутаций (TanStack Query)
  api/events.ts         # SSE /v1/events/stream → инвалидация кэша; статус соединения
  api/types.ts          # типы API демона (порт web/src/lib/types.ts)
  servers/ServerContext.tsx  # список серверов + активный (AsyncStorage); токены устройств — servers/tokens.ts (SecureStore)
  servers/pairing.ts    # parsePairLink, pairWithServer (POST /v1/auth/pair), usePairing
  api/chat.ts           # лента чата: хвост + инкрементальный курсор, дедупликация
  components/           # ui-kit по токенам дизайна (theme.ts)
```

Данные: SSE-события демона инвалидируют кэш TanStack Query (поллинг остаётся медленным safety-net'ом; при обрыве стрима — быстрый поллинг и баннер под хедером). Общение с агентами — через чат (`GET /v1/sessions/{id}/chat`, зеркало нативного транскрипта агента) с отправкой через очередь сообщений; терминала в мобильном приложении нет.

Дизайн — мобильные макеты из claude.ai/design проекта «Модульная структура дашборда» (`*.mobile.dc.html`); токены в `src/theme.ts`.
