# Скилл брейншторма 1.1: сначала дерево фактов, сценарии перед спекой (задача #5027)

## Коротко

**Что делаем.** Две правки в тексте скилла `orchestrator-brainstorming`, после которых он становится версией **1.1**:

1. До первого вопроса шторма оркестратор читает корень дерева фактов репо задачи (`INDEX.md`; если задача касается платформы — ещё и `lepsto/platform/INDEX.md`), спускается только по нужным веткам и пишет в «Проблеме» короткий список «что уже есть по теме» со ссылками. Нет `INDEX.md` — берёт вход в документацию репо и честно пишет, что дерева нет.
2. Перед спекой прогоняет 2–3 конкретных сценария через решение и записывает их в спеку: шаги и где решение не справляется.

Плюс минимальное версионирование, чтобы `rocket stats brainstorm` отличал 1.1 от базы: задача при старте запоминает `orchestrator-brainstorming@1.1`; прошлые штормы на своём скилле разовой миграцией помечаются `@1.0`. Если идущий шторм на 1.0 восстанавливают после обновления, он продолжает на тексте 1.1, метка остаётся 1.0, а в лог задачи пишется строка об этом.

3. **Починка (по просьбе cto после шторма):** рекомендованный вариант вопроса шторма виден агентам. Сейчас агент-участник (например, cto) получает только текст вопроса — без вариантов и без отметки рекомендации, а `rocket task show` / `rocket questions` варианты печатают, но рекомендацию нет. Станет: в доставленном агенту вопросе есть строка вариантов, рекомендованный помечен `★ рекомендовано`; так же — в `rocket task show`, `rocket task questions` и `rocket questions`.

**Что сознательно не делаем.** Других изменений в скилле нет (идеи v2 ждут данных). Нет выбора версии в настройках и параллельного A/B: в бинаре одна версия своего скилла; база — прошлые задачи на 1.0 и штатный `superpowers:brainstorming` через существующую настройку. Не трогаем codex-оркестраторов (они и так на штатном скилле) и воркеров.

## Контекст (что уже есть)

- Скилл встроен в бинарь: `internal/prompts/skills/orchestrator-brainstorming/` (копия superpowers 6.4.1), раскладывается в `<worktree>/.claude/skills/orchestrator-brainstorming/` при каждом запуске и restore оркестратора claude-code (`internal/agent/claudecode/claudecode.go`, `syncOrchestratorSkill`).
- `tasks.brainstorm_skill` (миграция 0020) фиксируется при `POST /v1/tasks/{id}/start` из `prompts.BrainstormSkill(custom && ships)` и читается обратно `session.Manager.brainstormSkill` для промпта (`{{brainstorm_skill}}`) и `LaunchSpec.BrainstormSkill`.
- Метрика (`internal/store/brainstorm_stats.go`) и экран `/brainstorm` (`BrainstormMetricsScreen.tsx`, цвета по `KNOWN_SKILLS`) группируют по строке `brainstorm_skill` как есть.

## Решения шторма

- Q1 (cto) → старые `orchestrator-brainstorming` переписываются миграцией в `orchestrator-brainstorming@1.0`; миграция идемпотентна, `superpowers:brainstorming` и `''` не трогает; в README скилла — таблица версий.
- Q2 (cto) → нет `INDEX.md` в репо задачи — вход в документацию репо (CLAUDE.md/README/оглавление `docs/`) и код, тот же спуск по нужным веткам, в «Проблеме» строка «дерева фактов нет, источники: …». Шаг не пропускается.
- После шторма (cto) → в задачу включена починка: рекомендация видна в доставке агенту и в CLI (найдено в этом шторме: cto не видел рекомендаций Q1–Q3, хотя они сохранены).
- Q3 (cto) → только вперёд, одна версия в бинаре, без A/B. Restore задачи, начатой на старой версии: метка не меняется, в README скилла это задокументировано, и rocket пишет в лог задачи одну строку `note`: «скилл шторма обновлён 1.0→1.1 при restore».

## Дизайн

### 1. Текст скилла (`internal/prompts/skills/orchestrator-brainstorming/SKILL.md`)

Ровно две вставки, остальной текст не меняется.

**Правка 1 — новый раздел сразу после `## Establish Shared Understanding` (перед `<HARD-GATE>`):**

```markdown
## Context Before Questions

Before your first question to anyone, read what already exists:

1. Read the root of the fact tree of the task's repo — `INDEX.md` at the
   repo root — in full. Read it from your worktree or from origin, never
   from a possibly stale local mirror.
2. If the task touches the platform, also read the platform root
   `INDEX.md` of `lepsto/platform` in full (from origin, e.g.
   `gh api repos/lepsto/platform/contents/INDEX.md`).
3. Descend only the branches that touch the task: each fact links one
   level down; follow a link while it is relevant, stop when it is not.
4. If the repo has no `INDEX.md`, use its documentation entry point
   instead (CLAUDE.md, README, the docs index, then the code) and
   descend the same way. Never skip this step.
5. In the problem statement, add a short list "what already exists on
   this topic": one line per fact, each with a link to its source. If
   there was no fact tree, say so and name the sources you used.

Only then ask. A question whose answer is in those facts is not a
question — use the fact, and cite it.
```

И в чек-листах путей пункт «Explore project context» у Bounded и Architectural получает приписку: `— start with Context Before Questions (fact tree first)`.

**Правка 2 — новый пункт чек-листа Architectural между «Present design» и «Write design doc»** (нумерация дальше сдвигается):

```markdown
6. **Run scenarios** — take 2-3 concrete scenarios (a real user, a real
   input, an edge case) and walk each through the proposed design step
   by step; note where the design does not cope. Fix the design or record
   the gap. The spec gets a "Scenarios" section with each walk-through and
   its gaps.
```

И в «After the Design → Documentation» добавляется пункт: `- The spec includes a "Scenarios" section: 2-3 concrete walk-throughs, step by step, and where the design does not cope.` В диаграмме `Process Flow` — узел `"Run scenarios"` между `"User approves design?" -> yes` и `"Write design doc"`.

### 2. Версия скилла

- `internal/prompts/skills.go`: константа `CustomBrainstormSkillVersion = "1.1"`; новая функция `BrainstormSkillRecord(custom bool) string` → `"orchestrator-brainstorming@1.1"` или `"superpowers:brainstorming"` (у штатного версии нет — он вне rocket); `SkillName(s string) string` — часть до `@`.
- Старт задачи (`internal/api/tasks.go`) пишет в `brainstorm_skill` значение с версией.
- Везде, где значение используется как **имя скилла** — `session.Manager.brainstormSkill` (промпт и `LaunchSpec`), `syncOrchestratorSkill` — берётся `SkillName(...)`. В промпте оркестратора остаётся `orchestrator-brainstorming` (именно так скилл зовётся в frontmatter).
- Миграция `0021_brainstorm_skill_version.sql`: `UPDATE tasks SET brainstorm_skill = 'orchestrator-brainstorming@1.0' WHERE brainstorm_skill = 'orchestrator-brainstorming';` — идемпотентна (повторный прогон ничего не меняет), `superpowers:brainstorming` и `''` не затрагивает.
- Метрика группирует по полной строке — ряды `@1.0` и `@1.1` разные без изменений в `brainstorm_stats.go`.
- Веб `/brainstorm`: цвета по имени скилла с версией — `KNOWN_SKILLS` дополняется так, чтобы `@1.0`, `@1.1` и штатный имели свои стабильные цвета (известные версии своего скилла — по списку, неизвестные — серый «прочее», как сейчас).
- `README.md` скилла: таблица версий (1.0 — дословная копия superpowers 6.4.1; 1.1 — дерево фактов + сценарии), перечень отличий от источника, правило «правка смысла скилла = поднять `CustomBrainstormSkillVersion`» и оговорка про restore (сценарий С2).
- Документация: `docs/05-state.md`, `03-daemon-api.md`, `08-orchestrators.md`, `10-agents.md`, `11-dashboard.md`, `12-tasks.md`, `04-cli.md` — значения `brainstorm_skill` с версией.

### 3. Рекомендация видна агентам и в CLI

Факты: текст доставки собирает `participantFanOut` (`internal/api/threads.go:333`) как `threadPrefix(...) + " " + body`; для `ask` (`internal/api/questions.go:465`) body = `q.Body` — без `Options` и `RecommendedOption`. CLI: `renderThreadOptions` (`internal/cli/task.go:1224`) печатает `  варианты: 1) a  2) b` и рекомендацию не знает; `questionRow` (`task.go:164`) поле декодирует, `threadRow` (`internal/cli/questions.go`) — нет. API (`brainstormWire.recommended_option`) отдаёт его и в задаче, и во входящих.

- Общий форматтер (один на API и CLI, чтобы текст совпадал): строка `варианты: 1) A ★ рекомендовано  2) B` — рекомендованный вариант с суффиксом ` ★ рекомендовано`; без рекомендации — как сейчас.
- Доставка вопроса (`ask` в задаче, kind `question`): если у вопроса есть варианты, к доставленному тексту после тела добавляется пустая строка и строка вариантов. Реплики и ответы не меняются (там варианты уже не нужны; ответ по `--choose` и так несёт текст выбранного).
- CLI: `rocket task show` и `rocket task questions` (оба через `renderQuestions`) и `rocket questions` (`renderThreadInbox`) передают рекомендацию в форматтер; в `threadRow` добавляется `RecommendedOption *int`.
- Ролевые треды (`rocket agent ask`) рекомендации не имеют (шторм на роли не открывается) — не трогаем; варианты в их доставку тоже не добавляем (вне рамок).
- Тесты: доставка storm-вопроса с `--recommend 2` → текст у участника-агента содержит `2) B ★ рекомендовано` и не содержит ★ у других; вопрос без вариантов → текст доставки байт-в-байт как раньше; CLI-рендер `task show`/`questions` с рекомендацией и без.

### Ошибки и края

- Значение без `@` (штатный, старые пустые) — `SkillName` возвращает как есть.
- Restore старой задачи с `@1.0`: промпт называет `orchestrator-brainstorming`, в worktree ложатся файлы 1.1 (в бинаре одна копия). Метка задачи остаётся `@1.0`; в лог задачи пишется `note` «скилл шторма обновлён orchestrator-brainstorming 1.0→1.1 при restore (метка задачи не меняется)» — при каждом restore, где версия метки ≠ текущей (без версии в метке не бывает после миграции; штатный и `''` — не пишется). Принято (Q3).

### Тесты

- `prompts`: `BrainstormSkillRecord`, `SkillName`; SKILL.md содержит раздел `## Context Before Questions` и пункт `Run scenarios`; frontmatter по-прежнему `name: orchestrator-brainstorming`.
- `api`: старт задачи с включённой настройкой пишет `orchestrator-brainstorming@1.1`; на codex — `superpowers:brainstorming`.
- `session`: задача с `@1.1` и с `@1.0` рендерит промпт с `orchestrator-brainstorming` и `LaunchSpec.BrainstormSkill == "orchestrator-brainstorming"` — и при спавне, и при restore; restore задачи с `@1.0` пишет одну запись `note` в лог задачи, с `@1.1`/штатным/`''` — ни одной.
- `claudecode`: `syncOrchestratorSkill` кладёт скилл для имени без версии (существующий тест остаётся зелёным).
- `store`: миграция переписывает только `orchestrator-brainstorming`; `BrainstormStats` даёт отдельные ряды `@1.0` и `@1.1`.
- web: тест экрана метрик — разные цвета у `@1.0` и `@1.1`.

## Сценарии

**С1. Новая задача в rocket, настройка «свой скилл» включена, агент claude-code.**
1. Иван жмёт Start → `brainstorm_skill = orchestrator-brainstorming@1.1`.
2. Промпт: «invoke orchestrator-brainstorming»; в worktree лежит SKILL.md 1.1.
3. Оркестратор ищет `INDEX.md` в корне rocket — его нет → читает CLAUDE.md/README/`docs/`, в «Проблеме» пишет «дерева фактов нет; источники: …» и список фактов со ссылками на файлы.
4. Если задача касается платформы — читает `lepsto/platform/INDEX.md` через `gh api`, спускается по 1–2 веткам.
5. Перед спекой — раздел «Сценарии». Ответы cto/Ивана считаются в ряду `orchestrator-brainstorming@1.1`.
- *Где не справляется:* «касается платформы» решает сам агент — может не прочитать платформу, когда стоило. Контроль — ревью «Проблемы» человеком/cto; данных пока нет, правило не усложняем.

**С2. Задача, начатая вчера на 1.0, ещё в шторме; daemon обновили, оркестратор упал и восстановлен `rocket restore`.**
1. Миграция уже переписала метку на `orchestrator-brainstorming@1.0`.
2. `brainstormSkill` → `SkillName` → `orchestrator-brainstorming`; промпт и раскладка корректны.
3. В worktree ложится SKILL.md 1.1 — оставшаяся часть шторма идёт по 1.1, а в метрике считается в 1.0; в логе задачи появляется строка «скилл шторма обновлён 1.0→1.1 при restore», так что смешение видно при разборе метрики.
- *Где не справляется:* смешение версий у таких переходных задач. Объём — единицы задач в момент релиза; принято осознанно (Q3), задокументировано в README скилла.

**С3. Оркестратор на codex.**
1. Start → `ships = false` → `brainstorm_skill = superpowers:brainstorming` (без версии).
2. Ряд метрики — штатный скилл, как и раньше; правки 1.1 его не касаются.
- *Где не справляется:* codex-штормы не получают правил 1.1 — вне рамок задачи.

**С4. Оркестратор задаёт cto вопрос шторма с `--recommend 1` (ровно то, что случилось в этом шторме).**
1. `rocket task ask … --brainstorm --recommend 1 --to cto --option A --option B`.
2. cto получает `[#N/Q1 question from orch] <тело>` + пустая строка + `варианты: 1) A ★ рекомендовано  2) B`.
3. cto в `rocket task show N` видит тот же ★.
- *Где не справляется:* старые сообщения, уже доставленные, не перерисуются; ролевые треды без вариантов в доставке — вне рамок.

## Критерии приёмки

- SKILL.md отличается от предыдущей версии только описанными двумя вставками (+ приписки к «Explore project context», пункт Documentation и узел диаграммы).
- Новые старты на claude-code с включённой настройкой пишут `orchestrator-brainstorming@1.1`; старые записи стали `@1.0`.
- `rocket stats brainstorm` показывает `@1.0` и `@1.1` отдельными рядами; экран `/brainstorm` — разными цветами.
- Промпт оркестратора и раскладка скилла работают для `@1.0`, `@1.1` и штатного.
- Агент-участник получает вопрос шторма со строкой вариантов и `★ рекомендовано` у рекомендованного; `rocket task show`, `rocket task questions`, `rocket questions` показывают то же.
- `make test` (go + web) зелёный, CI зелёный.
