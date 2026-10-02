# Brainstorm metric: every task and who stormed — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The brainstorm metric counts every storm answer, attributed to whoever gave it (human or a persistent agent such as `cto`), and shows next to each storm who stormed and how the human received the spec gate.

**Architecture:** Computed on the fly, as before. `internal/store/brainstorm_stats.go` drops the human-only filter and keys weekly rows by (week, skill, answered_by). Per storm it adds `answered_by`, `by_answerer` and `first_try_go`, and counts `spec_changes` only before the first Go. API and CLI expose the new fields additively. Web and mobile render the new columns, rows and a participant switch.

**Tech Stack:** Go (store/api/cli, `go test`), React + TanStack Query + vitest + MSW (web/), Expo React Native + jest (mobile/).

**Spec:** `docs/superpowers/specs/2026-10-02-brainstorm-who-stormed-design.md`

**Decomposition into workers (one PR each):**
- **W1 `metric-backend`:** Tasks 1–3 (store, API, CLI, API docs).
- **W2 `metric-clients`:** Tasks 4–6 (web, mobile). It works against the API contract in Task 2 through types and MSW fixtures, so it can run in parallel with W1. The live check happens after both merge.

## Global Constraints

- The participant id on the wire is `"human"` for the human (never `""` or `"user"`). Agents use their id (`"cto"`).
- The UI shows `human` as «Иван» and an agent by its id, verbatim. A storm label joins participants with « + » in order of first answer.
- `answered_by` (per storm) and `by_answerer` are always JSON arrays, `[]` when nothing has been answered.
- `spec_changes` counts only gates with `status='changes'` decided before the first Go (`decided_at < go_at`). Without a Go it counts all `changes`. `superseded` and `pending` never count.
- `first_try_go = go_at != null && spec_changes == 0`.
- Gate state labels (RU, exact): `Go с 1-го раза`, `Go после N правок`, `ждёт Go (правок: N)`, `—` when the task has no gates. Clients cannot tell "no gates" from "pending with 0 changes", so the API adds `has_gate bool`.
- Only `type='brainstorm'` threads count. Decision questions stay out.
- Terminal-recorded answers (`brainstorm record`) are authored `human` and stay the human's.
- No migration and no aggregate table.
- The repo has no CI. The worker runs `make test` (and the web/mobile commands below) and pastes the output into the PR.

## Review Focus

1. **A mixed storm where the same participant answers twice:** `answered_by` lists that participant once, in first-answer order (Task 1, `TestStormMixedAnswerers`).
2. **A `changes` gate decided after Go:** it must not count, and `first_try_go` stays true (Task 1, `TestStormSpecChangesBeforeGo`).
3. **A gate superseded by a new spec version with no human decision:** not a change (Task 1, `TestStormSupersededIsNotAChange`).
4. **A stored switch value on the web screen naming a participant absent from the current window:** fall back to the default rule instead of showing an empty chart (Task 4).
5. **A legacy answer authored `""`:** counts as `human`, never as a separate participant (Task 1, existing legacy case extended to assert `AnsweredBy == ["human"]`).

---

### Task 1: Store — attribute every answer, per-answerer counts, spec changes before Go

**Files:**
- Modify: `internal/store/brainstorm_stats.go`
- Test: `internal/store/brainstorm_stats_test.go`

**Interfaces:**
- Produces:
  - `BrainstormWeek.AnsweredBy string`
  - `type BrainstormAnswererCounts struct { AnsweredBy string; Answered, Accepted, AcceptedWithComment, Corrected, WrongTurn int }`
  - `BrainstormStorm.AnsweredBy []string`, `BrainstormStorm.ByAnswerer []BrainstormAnswererCounts`, `BrainstormStorm.FirstTryGo bool`, `BrainstormStorm.HasGate bool`
  - `BrainstormCounts.SpecChanges`: same field, new meaning (before the first Go).

- [ ] **Step 1: Write failing tests.** Extend the fixture helpers already in `brainstorm_stats_test.go` (the tests at :133 and :257 build questions with an answer author and gates). Add:
  - `TestStormAgentAnswersCount`: a storm whose 3 questions were answered by `cto` (accepted). Expect `Answered==3`, `AnsweredBy==["cto"]`, `ByAnswerer==[{cto,3,3,0,0,0}]`, and a weekly row `{AnsweredBy:"cto", Answered:3}`.
  - `TestStormMixedAnswerers`: q1 by `human` at t1 (accepted), q2 by `cto` at t2 (corrected), q3 by `human` at t3 (wrong_turn). Expect `AnsweredBy==["human","cto"]`, `ByAnswerer[0]=={human,2,1,0,0,1}`, `ByAnswerer[1]=={cto,1,0,0,1,0}`, storm totals `Answered==3`, and two weekly rows (human, cto) for the same week and skill.
  - `TestStormSpecChangesBeforeGo`: gates `changes`@10, `go`@20, `changes`@30. Expect `SpecChanges==1`, `GoAt==20`, `FirstTryGo==false`. A second case with `go`@20 and `changes`@30 only: `SpecChanges==0`, `FirstTryGo==true`.
  - `TestStormSupersededIsNotAChange`: gates `superseded`@10, `go`@20. Expect `SpecChanges==0`, `FirstTryGo==true`, `HasGate==true`.
  - `TestStormNoGoCountsAllChanges`: `changes`@10, `changes`@20, `pending`@30. Expect `SpecChanges==2`, `GoAt==nil`, `FirstTryGo==false`, `HasGate==true`.
  - Update the existing assertions that expected the `cto` answer to be excluded (:146 and :263): the answer now counts under `cto`. Extend the legacy-`""` case to assert `AnsweredBy==["human"]`. The dismissed and reopened cases still don't count.
  - A storm with questions but no answers: `AnsweredBy` and `ByAnswerer` are non-nil empty slices.

- [ ] **Step 2: Run them and see them fail**

Run: `go test ./internal/store/ -run 'Brainstorm|Storm' -v`
Expected: FAIL (undefined fields, or old counts).

- [ ] **Step 3: Implement**

Replace `humanAnswer` with:

```go
// answerer is the participant whose answer closed q, canonical ("" → "human"),
// and whether q counts in the metric at all: answered with an outcome.
func (q stormQuestion) answerer() (string, bool) {
	if q.status != "resolved" || q.resolution != "answered" || q.outcome == "" || !q.answerAuthor.Valid {
		return "", false
	}
	return canonicalParticipant(q.answerAuthor.String), true
}
```

Make `count` work on a small shared struct so that storms, answerers and weeks all reuse it:

```go
// BrainstormAnswers are the answer counters; Answered = Accepted + Corrected + WrongTurn.
type BrainstormAnswers struct {
	Answered, Accepted, AcceptedWithComment, Corrected, WrongTurn int
}
func (c *BrainstormAnswers) count(q stormQuestion) { /* body of today's count */ }

type BrainstormAnswererCounts struct {
	AnsweredBy string
	BrainstormAnswers
}
```

Embed `BrainstormAnswers` in `BrainstormCounts` (next to `Questions` and `SpecChanges`) and in `BrainstormWeek`, so that the existing field names `Answered`, `Accepted` and so on still resolve.

In `buildStorms`:
- Sort `qs` by `(resolvedAt, id)` before folding. Add `id int64` to `stormQuestion` and `q.id` to the SELECT. This makes first-answer order deterministic.
- For each answered question: `st.count(q)`. Find or append the answerer's entry in `st.ByAnswerer` (append order is first-answer order) and count there. Append to `st.AnsweredBy` when the answerer is new.
- Gates, in two passes:
  1. Find the first Go, set `HasGate=true` for any gate, and track `LastActivity`.
  2. `SpecChanges` = the `changes` gates where `goAt == nil || (decidedAt.Valid && decidedAt.Int64 < *goAt)`.
- `FirstTryGo = GoAt != nil && SpecChanges == 0`.
- `newStorm` initialises `AnsweredBy: []string{}` and `ByAnswerer: []BrainstormAnswererCounts{}`.

In `BrainstormStats`:
- `weekKey{week, skill, answeredBy}`.
- Sort: week, then skill, then `human` first, then other answerers ascending.

Update the comments on `BrainstormWeek`, `BrainstormCounts` and the file header: "every answer counts, attributed to its author".

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/ -v -run 'Brainstorm|Storm'` and then `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit** — `git commit -m "store: brainstorm metric counts every answer by its author; spec changes before Go (task-5019)"`

### Task 2: API — new fields on the stats endpoints, API docs

**Files:**
- Modify: `internal/api/stats.go`, `docs/03-daemon-api.md` (the brainstorm stats section, ≈:194, where "only the human's answers" is stated)
- Test: `internal/api/stats_test.go`

**Interfaces:**
- Consumes: the Task 1 store types.
- Produces (wire, exact):
  - week: `{week, skill, answered_by, answered, accepted, accepted_with_comment, corrected, wrong_turn}`
  - storm: the old fields plus `answered_by: string[]`, `by_answerer: [{answered_by, answered, accepted, accepted_with_comment, corrected, wrong_turn}]`, `first_try_go: bool`, `has_gate: bool`
  - `GET /v1/tasks/{id}/brainstorm/stats` returns the same storm object.

- [ ] **Step 1: Write failing tests** in `stats_test.go`:
  - Seed a storm answered by `cto` with a Go gate. Decode both endpoints into `map[string]any`. Assert `answered_by==["cto"]`, `by_answerer[0].answered_by=="cto"`, `first_try_go==true`, `has_gate==true`, and a weekly row with `answered_by=="cto"`.
  - Seed a task with no storm. `answered_by` and `by_answerer` decode as `[]any{}` (not nil), `has_gate==false`.
- [ ] **Step 2: Run** `go test ./internal/api/ -run Stats -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** Add `AnsweredBy string \`json:"answered_by"\`` to `brainstormWeekResponse`. Add `brainstormAnswererResponse` and the four new storm fields, mapped in `toBrainstormStormResponse`. Map the slices with `make(..., len)` so that empty slices encode as `[]`.
- [ ] **Step 4: Update `docs/03-daemon-api.md`.** Add the new fields. Replace the "only the human's answers count" rule with the §2 definitions from the spec: answer attribution, `spec_changes` before the first Go, `first_try_go`, `has_gate`.
- [ ] **Step 5: Run** `go test ./internal/api/`. Expected: PASS. **Commit:** `api: brainstorm stats expose answered_by, by_answerer, first_try_go, has_gate (task-5019)`

### Task 3: CLI — who column, who-stormed column, gate column

**Files:**
- Modify: `internal/cli/stats.go`
- Test: `internal/cli/stats_test.go`

**Interfaces:**
- Consumes: the Task 2 wire fields. Extend the CLI's `brainstormStats` decoding structs the same way.
- Produces: `participantLabel(id string) string` ("human"→"Иван", otherwise id), `stormWho(ids []string) string` (labels joined with " + ", "—" if empty), `gateState(goAt *int64, specChanges int, hasGate bool) string` (the exact RU labels from Global Constraints; for "Go после N правок" use the number verbatim).

- [ ] **Step 1: Write failing tests:**
  - table-driven tests for `participantLabel`, `stormWho` (`[]`→"—", `["human","cto"]`→"Иван + cto") and `gateState` (all four states);
  - a render test: the weekly header is `НЕДЕЛЯ  СКИЛЛ  КТО  ОТВЕЧЕНО …`, and a cto row shows `cto`;
  - the storms header contains `КТО ШТОРМИЛ`, `ПРАВОК ДО GO`, `ГЕЙТ`, `GO`, and a row shows `cto` and `Go с 1-го раза`.
- [ ] **Step 2: Run** `go test ./internal/cli/ -run Stats -v`. Expected: FAIL.
- [ ] **Step 3: Implement** in `renderBrainstormStats`:
  - weekly columns: `НЕДЕЛЯ СКИЛЛ КТО ОТВЕЧЕНО ПРИНЯТО С КОММЕНТАРИЕМ ПОПРАВЛЕНО НЕ ТУДА`;
  - storm columns: `ЗАДАЧА СКИЛЛ КТО ШТОРМИЛ ВОПРОСОВ ОТВЕЧЕНО ПРИНЯТО С КОММЕНТАРИЕМ ПОПРАВЛЕНО НЕ ТУДА ПРАВОК ДО GO ГЕЙТ GO НАЗВАНИЕ`.
- [ ] **Step 4: Run** `make test`. Expected: PASS (Go and web). Run `go build ./... && go run ./cmd/rocket stats brainstorm --help`.
- [ ] **Step 5: Commit** `cli: stats brainstorm shows who answered, who stormed and the gate state (task-5019)`. Open the PR with the `make test` output.

### Task 4: Web — metrics screen switch and columns

**Files:**
- Modify: `web/src/lib/types.ts` (the stats types, ≈:652-684), `web/src/mocks/fixtures.ts` (brainstorm stats fixtures ≈:876+), `web/src/screens/brainstorm/BrainstormMetricsScreen.tsx`
- Create: `web/src/lib/brainstormWho.ts` (+ `brainstormWho.test.ts`): `participantLabel`, `stormWho`, `gateState`, the TS twins of the Task 3 helpers with the same exact labels.
- Test: `web/src/screens/brainstorm/BrainstormMetrics.test.tsx`

**Interfaces:**
- Consumes: the Task 2 wire contract (copy it into the TS types verbatim): `BrainstormWeek.answered_by: string`, `BrainstormStorm.answered_by: string[]`, `by_answerer: BrainstormAnswerer[]`, `first_try_go: boolean`, `has_gate: boolean`.
- Produces: `brainstormWho.ts` exports used by Task 5: `participantLabel(id: string): string`, `stormWho(ids: string[]): string`, `gateState(s: {go_at: number|null; spec_changes: number; has_gate: boolean}): string`.

- [ ] **Step 1: Write failing tests:**
  - `brainstormWho.test.ts`: the same table as Task 3.
  - Screen tests (fixtures have weekly rows for `human` and `cto`, and storms answered by cto, by human, and mixed):
    - the switch shows «Иван» and «cto», with Иван selected by default;
    - clicking «cto» changes the chart series data (assert by the rendered accessible labels or values the current chart test already uses);
    - with no `human` rows the first participant is selected;
    - a value in `localStorage` key `rocket.brainstormMetrics.answerer` is restored when it exists in the window. If it names a participant not present in the window, the default rule applies (Review Focus 4);
    - the storms table has the columns «Кто штормил» («Иван + cto» for the mixed storm), «Правок до Go» and «Гейт» («Go с 1-го раза»).
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/brainstormWho.test.ts src/screens/brainstorm` — FAIL.
- [ ] **Step 3: Implement:**
  - a segmented control above the chart (reuse the app's existing segmented or tab control if there is one; otherwise use a `role="radiogroup"` of buttons);
  - participants = the unique `answered_by` of the weekly rows in order (human first, then ascending);
  - wrap `localStorage` access in try/catch;
  - filter the weekly rows by the selected participant before the existing per-skill series code.
- [ ] **Step 4: Run** `cd web && npm test -- --run && npm run build` — PASS.
- [ ] **Step 5: Commit** `web: brainstorm metrics — participant switch, who stormed and gate columns (task-5019)`

### Task 5: Web — task Brainstorm tab and closed storm question

**Files:**
- Modify: `web/src/screens/task/BrainstormTab.tsx` (counters ≈:47-71), `web/src/components/BrainstormResult.tsx`
- Test: `web/src/screens/task/BrainstormTab.test.tsx`, `web/src/components/BrainstormResult.test.tsx`

**Interfaces:**
- Consumes: `participantLabel`, `stormWho`, `gateState` from Task 4. Also the thread's `answered_by` (already in `BrainstormFields`, `web/src/lib/types.ts` ≈:411-433).

- [ ] **Step 1: Write failing tests:**
  - the tab with a cto storm shows «Кто штормил: cto» and «Гейт: Go с 1-го раза»;
  - with a mixed storm it shows a per-participant counter row for «Иван» and for «cto»;
  - with no answers it shows «Кто штормил: —»;
  - `BrainstormResult` with `answered_by: "cto"` shows «ответил: cto», and with `"human"` shows nothing extra.
- [ ] **Step 2: Run** `cd web && npx vitest run src/screens/task/BrainstormTab.test.tsx src/components/BrainstormResult.test.tsx` — FAIL.
- [ ] **Step 3: Implement.** Keep the existing counters as storm totals. Add the who and gate lines, and the per-participant rows only when `by_answerer.length > 1`.
- [ ] **Step 4: Run** `cd web && npm test -- --run && npm run build` — PASS.
- [ ] **Step 5: Commit** `web: storm tab shows who stormed, gate state, per-participant counts (task-5019)`

### Task 6: Mobile — storm tab

**Files:**
- Modify: `mobile/src/api/types.ts` (≈:214, :431-447), `mobile/src/components/BrainstormTab.tsx` (:16-23), `mobile/src/lib/brainstorm.ts` (add `participantLabel`, `stormWho`, `gateState` with the exact labels from Global Constraints), and the closed storm question component that renders the result (find it via `mobile/src/lib/storm.ts` usages)
- Test: `mobile/__tests__/task/brainstorm-tab.test.tsx`, `mobile/__tests__/task/brainstorm-question.test.tsx`, `mobile/src/lib/storm.test.ts` (or a new `brainstorm.test.ts` next to the lib)

- [ ] **Step 1: Write failing tests:** the helpers table; the tab shows «Кто штормил: cto» and «Гейт: Go с 1-го раза»; a closed question answered by cto shows «ответил: cto».
- [ ] **Step 2: Run** `cd mobile && npm test -- brainstorm` — FAIL.
- [ ] **Step 3: Implement** to match the web behaviour (per-participant rows only when there is more than one participant).
- [ ] **Step 4: Run** `cd mobile && npm test && npm run typecheck` — PASS.
- [ ] **Step 5: Commit** `mobile: storm tab shows who stormed and gate state (task-5019)`. Open the PR (W2) with the web and mobile test and build output.

### Orchestrator verification after both merges

- `make test`, `cd web && npm test -- --run && npm run build`, `cd mobile && npm test && npm run typecheck` on `origin/main`.
- Live check with `rocket stats brainstorm` after the daemon picks up the new build:
  - #5009 shows `cto`, answered 2;
  - #5010 shows `cto`, answered 3;
  - both show `Go с 1-го раза`;
  - a `2026-W40 … cto` weekly row exists.
