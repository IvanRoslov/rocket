# orchestrator-brainstorming

Скилл шторма оркестратора rocket (задача #4901, спека
`docs/superpowers/specs/2026-10-01-orchestrator-brainstorm-design.md`, §1;
версия 1.1 — задача #5027, спека
`docs/superpowers/specs/2026-10-02-brainstorm-skill-1-1-design.md`).

## Версии

| Версия | Что | Задача |
|---|---|---|
| 1.0 | Дословная копия `skills/brainstorming/` из superpowers 6.4.1 (только переименование) — база метрики | #4901 |
| 1.1 | + «Context Before Questions» (дерево фактов до первого вопроса, список «что уже есть» в «Проблеме»); + пункт «Run scenarios» перед спекой (раздел «Сценарии») | #5027 |

Версия, с которой стартовала задача, хранится в `tasks.brainstorm_skill` как
`orchestrator-brainstorming@<версия>` (константа `CustomBrainstormSkillVersion`
в `internal/prompts/skills.go`). Любая правка смысла скилла = новая версия:
поднять константу и добавить строку сюда.

В бинаре одна копия — последняя версия. Restore задачи, начатой на более
старой версии, кладёт в worktree текущий текст; метка задачи при этом не
меняется, а rocket пишет в лог задачи одну запись `note` («скилл шторма
обновлён orchestrator-brainstorming 1.0→1.1 при restore»).

## Источник

Основа — `skills/brainstorming/` из плагина **superpowers 6.4.1**
(<https://github.com/obra/superpowers>, MIT, © 2025 Jesse Vincent — текст
лицензии в `LICENSE` рядом).

Отличия от источника:

- `SKILL.md`, frontmatter: `name: brainstorming` → `name: orchestrator-brainstorming`;
- `SKILL.md`, путь к собственному файлу: `skills/brainstorming/visual-companion.md`
  → `.claude/skills/orchestrator-brainstorming/visual-companion.md`
  (так он разрешается из корня worktree оркестратора);
- 1.1: раздел `## Context Before Questions` перед `<HARD-GATE>` (корень дерева
  фактов `INDEX.md` репо задачи и, если задача касается платформы,
  `lepsto/platform`; спуск только по нужным веткам; без `INDEX.md` — вход в
  документацию репо; список «what already exists on this topic» в «Проблеме»);
  пункт «Explore project context» у Bounded и Architectural начинается с него;
- 1.1: пункт чек-листа Architectural `**Run scenarios**` между «Present design»
  и «Write design doc» (нумерация дальше сдвинута), узел `"Run scenarios"` в
  `Process Flow`, пункт про раздел «Scenarios» в «After the Design →
  Documentation».

`README.md` и `LICENSE` добавлены rocket.

## Как попадает к агенту

Каталог встроен в бинарь (`internal/prompts/skills.go`). При подготовке
рабочей папки оркестратора на claude-code, если задача стартовала с
настройкой `orchestrator_brainstorm_custom = true`, rocket кладёт его в
`<worktree>/.claude/skills/orchestrator-brainstorming/` (перезаписывая каждый
раз; `scripts/*.sh` — исполняемые) и исключает из git через
`.git/info/exclude`. Иначе каталог из worktree удаляется. Глобальный
`~/.claude` не трогается; воркеры скилл не получают.

Менять содержание скилла — по правилу версий выше (раздел «Версии»).
