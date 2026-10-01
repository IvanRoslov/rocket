# orchestrator-brainstorming

Скилл шторма оркестратора rocket (задача #4901, спека
`docs/superpowers/specs/2026-10-01-orchestrator-brainstorm-design.md`, §1).

## Источник

Дословная копия `skills/brainstorming/` из плагина **superpowers 6.4.1**
(<https://github.com/obra/superpowers>, MIT, © 2025 Jesse Vincent — текст
лицензии в `LICENSE` рядом). Это базовая линия для метрики качества
рекомендаций: по смыслу скилл не менялся.

Отличия от источника — только переименование:

- `SKILL.md`, frontmatter: `name: brainstorming` → `name: orchestrator-brainstorming`;
- `SKILL.md`, путь к собственному файлу: `skills/brainstorming/visual-companion.md`
  → `.claude/skills/orchestrator-brainstorming/visual-companion.md`
  (так он разрешается из корня worktree оркестратора).

`README.md` и `LICENSE` добавлены rocket.

## Как попадает к агенту

Каталог встроен в бинарь (`internal/prompts/skills.go`). При подготовке
рабочей папки оркестратора на claude-code, если задача стартовала с
настройкой `orchestrator_brainstorm_custom = true`, rocket кладёт его в
`<worktree>/.claude/skills/orchestrator-brainstorming/` (перезаписывая каждый
раз; `scripts/*.sh` — исполняемые) и исключает из git через
`.git/info/exclude`. Иначе каталог из worktree удаляется. Глобальный
`~/.claude` не трогается; воркеры скилл не получают.

Менять содержание скилла — отдельные истории майлстона #4900; при
обновлении источника замените файлы целиком и обновите версию здесь.
