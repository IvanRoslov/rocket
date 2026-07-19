# Полевые улучшения №1 (после первого боевого прогона) — план

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax.

**Goal:** Устранить четыре полевых дефекта: потеря больших брифов в TUI-гонке Claude Code, слепота отправителя к судьбе сообщения, загрязнение PR файлом `.claude/settings.json`, дыры в промпт-гейтах (спека/评обоснование, «спорь с доказательствами»).

**Root cause (диагностировано):** большие пасты (~6KB/60+ строк) не сворачиваются Claude Code в плейсхолдер; немедленный Enter гонится с регистрацией пасты — интермиттентно ход модели строится из пустого черновика при полном тексте в транскрипте. Не баг rocket; обходится доставкой больших сообщений файлом.

## Global Constraints

- Формат событий/error envelope/тесты/vet/gofmt — как всегда; сабагентам запрещены merge/rebase/reset.
- Изменения промптов синхронно в internal/prompts/templates/ И docs/prompts/*.md (референс).
- Порог «большого» сообщения: `large_message_threshold` config, default 2048 байт ИЛИ >20 строк.

### Task 1: Inbox-доставка больших сообщений

**Files:** internal/queue/queue.go, internal/config, docs/06-messaging.md (+абзац), tests.

- В deliver(): если len(body) > threshold (байты или строки) И у получателя есть WorktreePath — записать тело в `<worktree>/.rocket/inbox/msg-<id>.md` (mkdir 0755, файл 0644, atomic) и инжектировать вместо тела короткий указатель: `<prefix>[large message] Full text written to .rocket/inbox/msg-<id>.md — read that file now.` (prefix = существующий `[from X] `). Маленькие — как раньше. Файл НЕ удаляется после доставки (история; .rocket/ уже в git-exclude после Task 3).
- Fallback: запись файла не удалась → доставить полным телом по-старому (лог warn).
- Defense-in-depth в runtime.Inject: после markerSeen, перед первым Enter — settle-пауза `min(2s, 200ms + 5ms*строк)` только когда строк > 20 (снижает гонку для средних паст, которые всё же поехали пастой).
- [ ] TDD queue (порог, файл+указатель, fallback, маленькие нетронуты); runtime settle (тайминг-инвариант через фейковые часы не нужен — проверить, что для больших текстов пауза вызвана: вынести в поле sleepFn).
- [ ] Commit: `feat(queue): deliver large messages via worktree inbox file`.

### Task 2: Фидбек отправителю

**Files:** internal/queue/queue.go (fail-путь), tests.

- В fail(): если msg.FromSession != "" и сессия-отправитель жива (spawning|running) — поставить ей в очередь уведомление `[rocket] delivery FAILED: message #<id> to <to> (<reason>). Body preserved in queue history (rocket send --wait next time for critical messages).` Без рекурсии: уведомление помечать from="" (системное) и НЕ уведомлять о фейле уведомления (guard по префиксу или отдельный флаг колонки не нужен — from="" уже не триггерит).
- [ ] TDD: фейл доставки → отправителю приезжает нотис; фейл нотиса → без рекурсии; мёртвый отправитель → тихо.
- [ ] Commit: `feat(queue): notify sender on delivery failure`.

### Task 3: settings.local.json + git-exclude плюмбинга

**Files:** internal/agent/claudecode/claudecode.go (+тесты).

- Hooks писать в `.claude/settings.local.json` (тот же идемпотентный merge; НЕ трогать `.claude/settings.json` больше). Существующий пользовательский settings.local.json — merge с сохранением чужих ключей (как раньше).
- git-exclude в worktree (через `git rev-parse --git-path info/exclude`, как в codex): строки `.claude/settings.local.json`, `.rocket/`, `.rocket-prompt.md`, `.rocket-launch.sh` — идемпотентно, только для НЕтрекаемых путей (settings.local.json почти всегда нетрекаем).
- [ ] TDD: hooks в local; settings.json не создаётся/не меняется; exclude-строки появились один раз; повторный вызов идемпотентен.
- [ ] Commit: `fix(claudecode): hooks in settings.local.json, exclude rocket plumbing from git`.

### Task 4: Промпт-гейты

**Files:** internal/prompts/templates/{orchestrator,kickoff,worker}.md, docs/prompts/*.md (sync), prompts-тесты (обновить снапшот-фразы).

- kickoff.md gate 3 → усилить: «Store the spec, then ask for confirmation THROUGH THE TASK: `rocket task ask <id> "Confirm spec v<N> (task doc): ok to start implementation?"`. A chat "yes" about the design is NOT spec confirmation. Do not spawn until the question is answered. ANY later edit to the spec — including rationale-only edits — reopens this gate: store the new version and ask again.»
- orchestrator.md «Tracking the task» — то же правило одной строкой (правка спеки → переподтверждение).
- worker.md Ground rules +: «The brief may be wrong. Verify every claim against the actual code; when file:line in the brief and the code disagree, the code wins. If an instruction (including from your orchestrator) would destroy work and your own verification contradicts its premise — do not execute it; reply with your evidence instead.» + примечание про inbox: «Large messages arrive as a pointer to .rocket/inbox/msg-N.md — read the file immediately.»
- [ ] TDD: рендеры содержат новые фразы; StripSkills-инварианты не сломаны.
- [ ] Commit: `feat(prompts): spec confirmation gate, evidence-based pushback, inbox note`.

### Task 5: Live-проверка inbox-доставки

- Живой claude-воркер, `rocket send --file` с 6KB-брифом 3 раза подряд → каждый раз воркер читает файл и пересказывает содержимое (проверка по терминалу); маленькое сообщение — по-старому. Зачистка. Фиксы отдельными коммитами.
- [ ] Commit(ы) по фактам.

## Self-Review (выполнен)

- Все четыре пункта фидбека покрыты (CI-гейт исключён по решению пользователя). Корень A обойдён архитектурно (inbox), гонка Claude Code дополнительно демпфирована settle-паузой. B не рекурсирует. C повторяет проверенный exclude-паттерн codex. D/E — синхронно с референсными доками.
