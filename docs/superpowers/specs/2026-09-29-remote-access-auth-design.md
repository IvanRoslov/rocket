# Удалённый доступ и аутентификация — дизайн

Дата: 2026-09-29. Статус: на ревью.

## Зачем

Сейчас `rocketd` слушает `0.0.0.0:4477` (http) и `:4478` (https, self-signed) **без какой-либо
аутентификации**. Любой в локальной сети может:

- открыть веб-терминал `/v1/sessions/{id}/term` — это shell на Mac;
- слать сообщения агентам, которые работают с полными правами;
- читать настройки (включая GitHub-токен);
- подставить `X-Rocket-Session` и выступать от имени любого агента.

Это было приемлемо, пока Mac жил дома. Цель — закрыть демон и при этом получить доступ из
мобильного приложения и браузера **откуда угодно**.

**Критерий успеха:** с телефона по 4G мобилка и дашборд работают полностью (включая SSE и
терминал); посторонний — в той же Wi-Fi или в интернете — не может сделать ни одного запроса к API.

## Принятые решения

| Вопрос | Решение |
|---|---|
| Транспорт снаружи | **Tailscale**: телефон и Mac в одной tailnet |
| Интерфейсы демона | Только `127.0.0.1`; наружу — `tailscale serve` (настоящий сертификат `*.ts.net`, HTTP/2) |
| Модель входа | **Токены устройств** с одноразовым кодом сопряжения (QR / ссылка), отзыв по устройству |
| Хранение токенов | Непрозрачные случайные токены, в SQLite только SHA-256 (JWT отклонён: нужен мгновенный отзыв, демон один) |

Вне объёма: пароли, роли/мультипользователь, Cloudflare Tunnel, Tailscale-identity-заголовки
вместо токенов (с localhost их подделает любой процесс).

## 1. Модель доверия

| Канал | Кто | Аутентификация |
|---|---|---|
| unix-сокет (0600) | CLI, агенты, хуки | Доверен, как сейчас. Только здесь работают `X-Rocket-Session` и `/v1/internal/*` |
| TCP (`port`, `tls_port`) | браузер, мобилка, `tailscale serve` | **Всегда** нужен токен устройства — даже с 127.0.0.1 (`tailscale serve` проксирует с localhost, источник не отличить) |

На TCP:

- запрос с заголовком `X-Rocket-Session` → `401 agent_header_forbidden`;
- `/v1/internal/*` → `404`;
- без токена доступны только `POST /v1/auth/pair`, `GET /v1/auth/status` и статика SPA
  (не `/v1/*`), чтобы открылась страница логина. Всё остальное, включая SSE
  `/v1/events/stream` и WS `/v1/sessions/{id}/term` → `401 unauthorized`.

Реализация: middleware в `internal/api`, который знает, с какого листенера пришёл запрос.
Каждый `http.Server` получает свой `ConnContext`/обёртку, кладущую в контекст метку
`transport=unix|tcp`. Существующий `callerSession` на TCP больше не вызывается с заголовком
(его отсекает middleware раньше).

### Извлечение токена

По порядку: `Authorization: Bearer <token>` → cookie `rocket_auth`. Сравнение — по
`sha256(token)` через lookup в `devices` (хэш уникальный индекс), `revoked_at IS NULL`.

### CSRF и DNS-rebinding

- `Host` каждого TCP-запроса должен быть в allowlist: `localhost`, `127.0.0.1`, `[::1]` (с любым
  портом) и хост из `public_url`. Иначе `421 bad_host`. Закрывает DNS-rebinding.
- Для запросов, аутентифицированных **cookie**, у небезопасных методов (POST/PUT/PATCH/DELETE)
  и у WS-апгрейда `Origin` обязан совпадать со схемой+хостом запроса (или с `public_url`).
  Иначе `403 bad_origin`. Bearer-запросы (мобилка) Origin не проверяют.
- Cookie: `HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=31536000`. При доступе по
  `http://localhost` флаг `Secure` не ставится (браузер иначе её не сохранит).

## 2. Хранилище

Миграция `internal/store/migrations/0015_devices.sql`:

```sql
CREATE TABLE devices (
  id           INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL,
  kind         TEXT    NOT NULL CHECK (kind IN ('mobile','web')),
  token_hash   TEXT    NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER,
  revoked_at   INTEGER
);

CREATE TABLE pairing_codes (
  code_hash  TEXT    PRIMARY KEY,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);
```

- Токен устройства: 32 байта `crypto/rand`, base64url, префикс `rkt_`.
- Код сопряжения: 8 символов из алфавита Crockford base32 (~40 бит), отображается как `XXXX-XXXX`,
  TTL 10 минут, одноразовый. Истёкшие коды чистятся при создании нового.
- `last_seen_at` обновляется не чаще раза в минуту на устройство (in-memory троттлинг).

## 3. API

| Метод | Путь | Доступ | Описание |
|---|---|---|---|
| POST | `/v1/auth/pairing-codes` | unix или авторизованный TCP | → `{code, expires_at, url}`; `url = public_url` (или пусто) |
| POST | `/v1/auth/pair` | открыт | `{code, name, kind}` → device. `kind=mobile` → `{device, token}`; `kind=web` → `Set-Cookie` + `{device}` |
| GET | `/v1/auth/status` | открыт | `{authenticated: bool, device?: {...}}` — SPA решает, показывать ли логин |
| POST | `/v1/auth/logout` | авторизованный | Отзывает текущее устройство, чистит cookie |
| GET | `/v1/auth/devices` | unix или авторизованный | Список (без хэшей), с флагом `current` |
| DELETE | `/v1/auth/devices/{id}` | unix или авторизованный | Отзыв (`revoked_at = now`) |

Ошибки `/pair`: `400 invalid_code` (нет / истёк / использован — неразличимо), `429 rate_limited`.
Rate-limit: не более 5 неудачных попыток в минуту глобально (демон персональный — per-IP за
прокси бессмыслен), in-memory.

После отзыва открытые SSE/WS устройства закрываются: хаб держит соединения по device id и
рвёт их при `DELETE`.

## 4. Конфиг

`~/.rocket/config.yaml`:

- `host` — дефолт остаётся `127.0.0.1`; у пользователя `0.0.0.0` → вернуть на loopback.
- новое `public_url` (строка, опционально) — например `https://mac.tail1234.ts.net`.
  Используется в QR/ссылках и в Host/Origin-allowlist. Валидация: абсолютный URL со схемой https
  (http допускается с warning).
- `tls_port` — без изменений; при `tailscale serve` его можно выставить в `0`.

Если `host` не loopback, демон пишет в лог warning при старте (auth всё равно действует).

## 5. CLI

- `rocket pair [--name <имя>]` — создаёт код через unix-сокет и печатает QR
  (`rocketmobile://pair?url=<public_url>&code=<code>`) в терминал, под ним код и URL текстом.
  Без `public_url` — предупреждение и только код.
- `rocket pair --web` — печатает `<public_url или http://localhost:port>/login?code=<code>`.
- `rocket devices ls` — таблица ID / NAME / KIND / CREATED / LAST SEEN.
- `rocket devices revoke <id>`.
- `rocket doctor` — проверки: `host` не loopback → warn; `public_url` не задан → info;
  `tailscale` не найден / `tailscale serve status` не проксирует на порт демона → warn.

QR в терминале — библиотекой уровня `github.com/mdp/qrterminal/v3`.

## 6. Веб-дашборд (`web/`)

- Экран `/login`: при `?code=` — сразу `POST /v1/auth/pair {kind:"web", name: <UA-кратко>}` →
  редирект на `/`; иначе поле ввода кода.
- При старте — `GET /v1/auth/status`; неавторизован → `/login`.
- Любой `401 unauthorized` в `req()` или обрыв SSE с 401 → `/login` (однократно, без циклов).
- Settings → «Устройства»: список, «Отозвать», «Выйти», «Подключить телефон» — создаёт код и
  рисует QR (`qrcode` npm) с таймером истечения.
- `fetch`/`EventSource`/WS менять не нужно: cookie того же origin уходит автоматически.

## 7. Мобилка (`mobile/`)

- Модель сервера `{name, host, port}` → `{id, name, baseUrl}`; миграция старых записей в
  `http://host:port` (токена у них нет → экран «подключить заново»).
- Добавление сервера: «Сканировать QR» (`expo-camera`) — парсит `rocketmobile://pair?url&code`;
  альтернативно ручной ввод URL + кода. Затем `POST /v1/auth/pair {kind:"mobile"}`.
- Токен — в `expo-secure-store` (ключ по id сервера), в AsyncStorage только метаданные.
- `client.ts` и `sse.ts` (XHR — заголовки поддерживает) добавляют `Authorization: Bearer`.
- На 401 — баннер «Доступ отозван или истёк — подключить заново» с переходом на сканер.
- Существующая scheme `rocketmobile` (app.json) используется, чтобы QR, отсканированный системной камерой,
  открывал приложение сразу на подтверждении.

## 8. Настройка у пользователя (док `docs/testing/remote-access.md`)

1. Установить Tailscale на Mac и телефон, войти в одну tailnet.
2. `tailscale serve --bg 4477` → получить `https://<mac>.<tailnet>.ts.net`.
3. `config.yaml`: `host: 127.0.0.1`, `public_url: https://<mac>.<tailnet>.ts.net`; `make restart`.
4. `rocket pair` → сканировать телефоном; `rocket pair --web` → открыть ссылку в браузере.
5. Проверка снаружи: телефон на 4G (Wi-Fi выкл.) — канбан, чат, SSE-обновления; из LAN без
   Tailscale — `curl http://<lan-ip>:4477` не соединяется.

## 9. Документация

Обновить `docs/03-daemon-api.md` (раздел Auth, коды ошибок), `docs/11-dashboard.md` (логин,
устройства), `docs/04-cli.md` (`pair`, `devices`), `mobile/README.md` (новый онбординг).

## 10. Тестирование

Go (`internal/api`):

- unix-сокет без токена → 200; TCP без токена → 401 на API, 200 на статику и `/v1/auth/status`;
- TCP с `X-Rocket-Session` → 401 даже с валидным токеном; `/v1/internal/*` по TCP → 404;
- валидный bearer → 200; отозванный → 401; cookie → 200;
- cookie + POST с чужим `Origin` → 403; bearer + чужой Origin → 200; чужой `Host` → 421;
- WS-апгрейд терминала без токена → 401;
- pair-код: успешный обмен, повторное использование, истечение, rate-limit 429;
- отзыв закрывает открытый SSE-стрим.

Store: миграция, CRUD устройств и кодов.

Web (vitest + msw): редирект на `/login` по 401, авто-обмен `?code=`, экран устройств.
Mobile (jest): парсинг QR-URL, миграция серверов, заголовок Bearer в client и sse, баннер на 401.

Живой E2E — за пользователем: реальный `tailscale serve` с телефона по 4G.

## Риски

- **Забытый `host: 0.0.0.0`**: auth всё равно действует, плюс warning в логе и `doctor`.
- **Локальный браузер на `http://localhost:4477`** теперь тоже требует логина — один раз через
  `rocket pair --web`.
- **Зависимость от Tailscale**: при его падении мобилка недоступна снаружи; демон и локальный
  CLI не затронуты.
