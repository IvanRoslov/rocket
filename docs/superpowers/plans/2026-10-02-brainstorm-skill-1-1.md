# Brainstorm skill 1.1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `orchestrator-brainstorming` 1.1 (fact tree before questions, scenarios before spec) and record the skill version per task so `rocket stats brainstorm` separates 1.1 from the 1.0 baseline.

**Architecture:** Two independent PRs in `IvanRoslov/rocket`. Task A edits only the embedded skill text and its README. Task B versions the stored value: tasks store `orchestrator-brainstorming@1.1`, a migration relabels old rows `@1.0`, every consumer that needs the skill *name* strips `@version`; metric grouping is unchanged (it groups by the full string), the web chart gets stable colors for versions.

**Tech Stack:** Go (daemon, store with embedded SQL migrations, `prompts` embed FS), React/TS (web, vitest).

**Spec:** `docs/superpowers/specs/2026-10-02-brainstorm-skill-1-1-design.md`

## Global Constraints

- Skill name in frontmatter and in the orchestrator prompt stays exactly `orchestrator-brainstorming`.
- Stored value for the custom skill on new starts: `orchestrator-brainstorming@1.1`. Stock stays `superpowers:brainstorming` (no version). `''` stays `''`.
- Migration rewrites only `brainstorm_skill = 'orchestrator-brainstorming'` → `'orchestrator-brainstorming@1.0'`; idempotent.
- SKILL.md changes: only the two insertions defined in the spec (plus the "Explore project context" suffixes, the Documentation bullet, the Process Flow node). Nothing else in the skill changes.
- One copy of the skill in the binary (no A/B, no version setting).
- Code, identifiers, commits in English; `docs/*.md` of rocket are written in Russian (follow surrounding style).

## Review Focus

- Restore of a task labelled `orchestrator-brainstorming@1.0` → prompt names `orchestrator-brainstorming`, skill dir is laid into the worktree (not removed), one `note` in the task log. Test in Task B.
- Task with `brainstorm_skill = ''` restored → falls back to current setting; with setting on yields name `orchestrator-brainstorming` and skill dir laid. Test in Task B.
- Migration run twice / on DB with only stock and empty values → no change. Test in Task B.
- Unknown future version (`orchestrator-brainstorming@1.2`) in the chart → gets a color and appears after known ones, no crash. Test in Task B.
- `SkillName("superpowers:brainstorming")` must not cut at `:` — only `@`. Test in Task B.

---

### Task A: Skill text 1.1 (worker `skill-text`)

**Files:**
- Modify: `internal/prompts/skills/orchestrator-brainstorming/SKILL.md`
- Modify: `internal/prompts/skills/orchestrator-brainstorming/README.md`
- Test: `internal/prompts/skills_text_test.go` (new file — do not edit `skills_test.go`, Task B edits it)

**Interfaces:** Consumes nothing. Produces SKILL.md containing the heading `## Context Before Questions` and the checklist item `**Run scenarios**`.

- [ ] **Step 1: Write failing test** in `internal/prompts/skills_text_test.go`:

```go
package prompts

import (
	"strings"
	"testing"
)

// Version 1.1 of the skill (task #5027): fact tree before questions,
// scenarios before the spec.
func TestOrchestratorSkillV11Text(t *testing.T) {
	b, err := orchestratorSkillFS.ReadFile(orchestratorSkillRoot + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"\nname: orchestrator-brainstorming\n",
		"## Context Before Questions",
		"`INDEX.md`",
		"lepsto/platform",
		"what already exists on",
		"**Run scenarios**",
		`"Scenarios" section`,
		`"Run scenarios" [shape=box];`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
	// The section sits between shared understanding and the hard gate.
	ctx := strings.Index(s, "## Context Before Questions")
	if ctx < strings.Index(s, "## Establish Shared Understanding") || ctx > strings.Index(s, "<HARD-GATE>") {
		t.Error("Context Before Questions is not between Establish Shared Understanding and <HARD-GATE>")
	}
	// Run scenarios comes after Present design and before Write design doc.
	arch := s[strings.Index(s, "**Architectural:**"):]
	if !(strings.Index(arch, "**Present design**") < strings.Index(arch, "**Run scenarios**") &&
		strings.Index(arch, "**Run scenarios**") < strings.Index(arch, "**Write design doc**")) {
		t.Error("Run scenarios is not between Present design and Write design doc")
	}
}
```

- [ ] **Step 2:** `go test ./internal/prompts/ -run TestOrchestratorSkillV11Text` → FAIL.

- [ ] **Step 3: Edit SKILL.md.** Insert, right before `<HARD-GATE>` (after the paragraph ending "…its accuracy and the opportunity to correct it matter."), this section verbatim:

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

In `## Checklist`: Bounded item 1 and Architectural item 1 become
`1. **Explore project context** — start with Context Before Questions (fact tree first); check files, docs, recent commits`.
Architectural list: insert after item 5 (`**Present design**`)

```markdown
6. **Run scenarios** — take 2-3 concrete scenarios (a real user, a real
   input, an edge case) and walk each through the proposed design step
   by step; note where the design does not cope. Fix the design or record
   the gap. The spec gets a "Scenarios" section with each walk-through and
   its gaps.
```

and renumber the rest 7–10. In `## Process Flow` dot graph: add node `"Run scenarios" [shape=box];` and replace edge `"User approves design?" -> "Write design doc" [label="yes"];` with `"User approves design?" -> "Run scenarios" [label="yes"];` and `"Run scenarios" -> "Write design doc";`. In `## After the Design` → `**Documentation:**` add after the first bullet group:
`- The spec includes a "Scenarios" section: 2-3 concrete walk-throughs, step by step, and where the design does not cope.`

- [ ] **Step 4: README.md.** Replace the "Источник" intro so it carries a version table:

```markdown
## Версии

| Версия | Что | Задача |
|---|---|---|
| 1.0 | Дословная копия `skills/brainstorming/` из superpowers 6.4.1 (только переименование) — база метрики | #4901 |
| 1.1 | + «Context Before Questions» (дерево фактов до первого вопроса, список «что уже есть» в «Проблеме»); + пункт «Run scenarios» перед спекой (раздел «Сценарии») | #5027 |

Версия, с которой стартовала задача, хранится в `tasks.brainstorm_skill` как `orchestrator-brainstorming@<версия>` (константа `CustomBrainstormSkillVersion` в `internal/prompts/skills.go`). Любая правка смысла скилла = новая версия: поднять константу и добавить строку сюда.

В бинаре одна копия — последняя версия. Restore задачи, начатой на более старой версии, кладёт в worktree текущий текст; метка задачи при этом не меняется.
```

Also say: restore of an older-version task writes a `note` to the task log («скилл шторма обновлён 1.0→1.1 при restore»). Keep the "Отличия от источника" list and extend it with the two 1.1 insertions. Keep "Как попадает к агенту". Replace the closing paragraph "Менять содержание скилла — …" with a pointer to the version rule above.

- [ ] **Step 5:** `go test ./internal/prompts/...` → PASS (incl. existing `TestOrchestratorSkillIsRenamedCopy`).
- [ ] **Step 6:** Commit `feat(prompts): orchestrator-brainstorming 1.1 — fact tree first, scenarios before spec (task-5027)`; PR to `main`.

---

### Task B: Skill version recorded per task (worker `skill-version`)

**Files:**
- Modify: `internal/prompts/skills.go`, `internal/prompts/skills_test.go`
- Create: `internal/store/migrations/0021_brainstorm_skill_version.sql` (use the next free number on `origin/main` at implementation time)
- Modify: `internal/api/tasks.go` (start: ~line 992–1013), `internal/session/manager.go` (`brainstormSkill`, ~line 591 & 663–682), `internal/agent/claudecode/claudecode.go` (`syncOrchestratorSkill`)
- Modify: `web/src/screens/brainstorm/BrainstormMetricsScreen.tsx` (+ its test), `web/src/mocks/fixtures.ts` if it holds skill strings
- Modify docs: `docs/03-daemon-api.md`, `04-cli.md`, `05-state.md`, `08-orchestrators.md`, `10-agents.md`, `11-dashboard.md`, `12-tasks.md`
- Tests: `internal/prompts/skills_test.go`, `internal/api/tasks_brainstorm_skill_test.go`, `internal/session/brainstorm_skill_test.go`, `internal/store/brainstorm_skill_test.go`, `internal/store/brainstorm_stats_test.go`, `web/src/screens/brainstorm/BrainstormMetrics.test.tsx`

**Interfaces:**
- Produces in `prompts`:
  - `const CustomBrainstormSkillVersion = "1.1"`
  - `func BrainstormSkillRecord(custom bool) string` → `"orchestrator-brainstorming@1.1"` | `"superpowers:brainstorming"`
  - `func SkillName(record string) string` → part before the first `@`; no `@` → unchanged.
  - `BrainstormSkill(custom bool)` keeps returning the bare name (unchanged).

- [ ] **Step 1: prompts tests (fail first)** — add to `skills_test.go`:

```go
func TestBrainstormSkillRecord(t *testing.T) {
	if got := BrainstormSkillRecord(true); got != "orchestrator-brainstorming@1.1" {
		t.Errorf("BrainstormSkillRecord(true) = %q", got)
	}
	if got := BrainstormSkillRecord(false); got != "superpowers:brainstorming" {
		t.Errorf("BrainstormSkillRecord(false) = %q", got)
	}
}

func TestSkillName(t *testing.T) {
	for in, want := range map[string]string{
		"orchestrator-brainstorming@1.1": "orchestrator-brainstorming",
		"orchestrator-brainstorming@1.0": "orchestrator-brainstorming",
		"orchestrator-brainstorming":     "orchestrator-brainstorming",
		"superpowers:brainstorming":      "superpowers:brainstorming",
		"":                               "",
	} {
		if got := SkillName(in); got != want {
			t.Errorf("SkillName(%q) = %q, want %q", in, got, want)
		}
	}
}
```

Run `go test ./internal/prompts/` → FAIL. Implement in `skills.go`:

```go
// CustomBrainstormSkillVersion is the version of the embedded
// orchestrator-brainstorming skill (see its README). A task stores the skill
// it started with as "<name>@<version>" so the brainstorm metric can tell
// versions apart; bump it with every change to the skill's meaning.
const CustomBrainstormSkillVersion = "1.1"

// BrainstormSkillRecord is the value a task stores in brainstorm_skill when
// it starts: the custom skill with its version, the stock skill as is.
func BrainstormSkillRecord(custom bool) string {
	if custom {
		return CustomBrainstormSkill + "@" + CustomBrainstormSkillVersion
	}
	return StockBrainstormSkill
}

// SkillName strips the "@version" suffix of a stored brainstorm_skill,
// leaving the skill name a prompt names and an agent lays out.
func SkillName(record string) string {
	name, _, _ := strings.Cut(record, "@")
	return name
}
```

Run → PASS.

- [ ] **Step 2: migration + store test (fail first).** In `internal/store/brainstorm_skill_test.go` add a test that opens a store, inserts tasks with `brainstorm_skill` values `orchestrator-brainstorming`, `superpowers:brainstorming`, `''`, `orchestrator-brainstorming@1.1`, re-runs the migration SQL (follow the pattern in `migrate_question_title_test.go` for running a single migration against existing rows), and asserts: first → `orchestrator-brainstorming@1.0`, the rest unchanged; running the SQL a second time changes nothing. Then create the migration:

```sql
-- Версия скилла шторма в задаче (задача #5027): свой скилл теперь пишется
-- как orchestrator-brainstorming@<версия>. Всё, что стартовало раньше на
-- своём скилле, — версия 1.0 (дословная копия superpowers 6.4.1).
-- Штатный superpowers:brainstorming и '' не трогаются. Идемпотентна.

UPDATE tasks SET brainstorm_skill = 'orchestrator-brainstorming@1.0'
WHERE brainstorm_skill = 'orchestrator-brainstorming';
```

In `brainstorm_stats_test.go` add a case: two tasks with `@1.0` and `@1.1`, one answered storm question each → `BrainstormStats` returns two week rows with those `Skill` values. Run `go test ./internal/store/` → PASS.

- [ ] **Step 3: start writes the versioned record.** In `tasks_brainstorm_skill_test.go` change expectations: setting on + claude-code → `orchestrator-brainstorming@1.1`; codex → `superpowers:brainstorming`; setting off → `superpowers:brainstorming`. Run → FAIL. In `internal/api/tasks.go` replace `prompts.BrainstormSkill(custom && ships)` with `prompts.BrainstormSkillRecord(custom && ships)` and update the comment block. Run → PASS.

- [ ] **Step 4: consumers use the name.** In `internal/session/brainstorm_skill_test.go` add cases: task with `orchestrator-brainstorming@1.0` and `@1.1` → rendered orchestrator prompt + kickoff contain `orchestrator-brainstorming` and not `@`; `LaunchSpec.BrainstormSkill == "orchestrator-brainstorming"` (use the fake agent the existing tests use). Task with `''` + setting on + claude-code → `orchestrator-brainstorming`. Run → FAIL. In `manager.go` keep `brainstormSkill()` returning the stored record (fallback stays `prompts.BrainstormSkill(custom)`), and at the call site:

```go
	brainstormSkill := prompts.SkillName(m.brainstormSkill(task, ag))
```

Do the same at the second call site in `fillRestorePrompt` (~line 958): `brainstormSkill := prompts.SkillName(m.brainstormSkill(task, ag))` — both the template var and `spec.BrainstormSkill` get the bare name.

Restore note (cto, Q3): in `fillRestorePrompt`, for an orchestrator whose stored record is `orchestrator-brainstorming@<v>` with `<v> != prompts.CustomBrainstormSkillVersion`, write one task log entry (best effort — a failed write is `slog.Warn`, never fails the restore):

```go
	if rec := task.BrainstormSkill; prompts.SkillName(rec) == prompts.CustomBrainstormSkill {
		if _, v, ok := strings.Cut(rec, "@"); ok && v != prompts.CustomBrainstormSkillVersion {
			if _, err := m.st.AddTaskLog(store.TaskLogEntry{TaskID: task.ID, Kind: "note", Author: "rocket",
				Body: fmt.Sprintf("скилл шторма обновлён %s %s→%s при restore (метка задачи не меняется)",
					prompts.CustomBrainstormSkill, v, prompts.CustomBrainstormSkillVersion)}); err != nil {
				slog.Warn("session: restore skill-version note failed", "task", task.ID, "error", err)
			}
		}
	}
```

Tests (fail first, in `brainstorm_skill_test.go`): restore of an orchestrator whose task has `@1.0` → `ListTaskLog(task.ID, "note")` has exactly one entry containing `1.0→1.1`; restore with `@1.1`, `superpowers:brainstorming`, `''` → none. Check `TaskLogEntry` field names in `internal/store/tasks.go:409-416` and adapt.

In `claudecode.go` make `syncOrchestratorSkill` compare `prompts.SkillName(spec.BrainstormSkill) == prompts.CustomBrainstormSkill` (defensive; add a `claudecode` skill test case with `"orchestrator-brainstorming@1.1"` → dir laid). Run `go test ./internal/...` → PASS.

- [ ] **Step 5: web colors.** In `BrainstormMetrics.test.tsx` add a case with weeks for `orchestrator-brainstorming@1.0`, `orchestrator-brainstorming@1.1`, `superpowers:brainstorming`, `orchestrator-brainstorming@1.2`: legend order is `@1.0, @1.1, superpowers:brainstorming, @1.2`, and `@1.0`/`@1.1`/stock swatches have three different backgrounds. Run `cd web && npx vitest run src/screens/brainstorm` → FAIL. Change:

```ts
// Color follows the skill, never its rank: each known skill (versions of
// our own skill included) owns a categorical slot; anything else (newer
// versions, "unknown") takes the last one.
const KNOWN_SKILLS = ['orchestrator-brainstorming@1.0', 'orchestrator-brainstorming@1.1', 'superpowers:brainstorming']
const SERIES_COLORS = ['var(--viz-series-1)', 'var(--viz-series-2)', 'var(--viz-series-3)', 'var(--viz-series-4)']

function skillColor(skill: string): string {
  const i = KNOWN_SKILLS.indexOf(skill)
  return SERIES_COLORS[i === -1 ? 3 : i]
}
```

Check `--viz-series-4` exists in the web theme CSS (`grep -rn "viz-series-4" web/src`); if it does not, define it next to `--viz-series-1..3` in both light and dark tokens. Update `fixtures.ts` skill strings to versioned ones. Run → PASS; `npm run lint` / `tsc` per `web/package.json`.

- [ ] **Step 6: docs.** In each listed doc replace the enumerations of `brainstorm_skill` values with `orchestrator-brainstorming@<версия>` (сейчас `@1.1`; до #5027 — `@1.0` после миграции 0021) | `superpowers:brainstorming` | `''`, and note that the prompt and the worktree get the name without `@version`. `05-state.md`: add migration 0021 line next to `brainstorm_skill`.

- [ ] **Step 7:** `make test` (Go + web) green. Commit `feat: record brainstorm skill version per task (orchestrator-brainstorming@1.1) (task-5027)`; PR to `main`.

---

## Order and merge

A and B are independent and run in parallel. Merge order does not matter: B's test does not read the skill text, A's test does not read the version. After both merge — `rocket verify-merge` for each subtask; spot-check: start a throwaway task in a dev daemon is NOT required; the tests cover it.
