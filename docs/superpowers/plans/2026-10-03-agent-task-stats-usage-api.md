# Agent Task Stats Usage API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose collected agent token usage and editable model prices through read APIs and `rocket stats` commands.

**Architecture:** Pure aggregation in `internal/usage` turns `store.UsageRow` and `store.ModelPrice` into JSON-ready summaries. HTTP handlers validate dates, query the store, aggregate rows, and enforce human-only price writes. CLI commands consume these response shapes and render tables or JSON.

**Tech Stack:** Go, SQLite store, `net/http` ServeMux, Cobra.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-task-stats-design.md` §3–4; W4 in `docs/superpowers/plans/2026-10-03-agent-task-stats.md`.

## Global Constraints

- Work only on W4. The collector and `stats collect` command belong to W3.
- `billable = input + cache_write + output`; `reasoning` is a subset of output.
- Prices are dollars per million tokens, keyed by exact model ID. A missing rate for a nonzero token category makes that model's cost null.
- Aggregate cost is the sum of priced models; `cost_partial` reports any unpriced model.
- Date filters are daemon-local calendar dates, both inclusive; store queries use `[from midnight, midnight after to)`.
- Price PUT and DELETE reject agent sessions with `403 human_only`.
- The binding W4 addendum requires `GET /v1/stats/prices` to return `{ "prices": [...] }`, with null rates and null `updated_at` for observed models lacking price rows. Draft W5 expects PUT to return the saved price object.
- A task session without `session_stats` has status `running` or `pending` from its state, `final:false`, `ended_at:null`, `duration_s:null`, `models:[]`, zero tokens, `cost_usd:null`, and `error:""`.
- Missing/error stats with no model rows have zero cost; task totals count collected sessions, while `sessions[]` also lists uncollected live and pending sessions.
- Session `pr_number` is null when absent and `pr_url` is an empty string when unresolved.
- The unlinked task bucket has null `task_id` and empty `title`, `status`, and `project_id`.

## Review Focus

- A zero-token model with incomplete prices should still have a zero cost; test in Task 1.
- Repeated model rows from one session should count that session once; test in Task 1.
- A date range crossing a local daylight-saving transition must use calendar midnights rather than fixed 24-hour offsets; test in Task 2.
- Invalid numeric values such as NaN or infinity, missing keys, and trailing JSON must not enter `model_prices`; test in Task 3.
- A model containing `/` or URL punctuation must remain one path segment in the prices CLI; test in Task 4.

---

### Task 1: Pure usage aggregation

**Files:** Create `internal/usage/aggregate.go`, `internal/usage/aggregate_test.go`; modify `internal/store/usage.go` and `internal/store/usage_test.go` to expose `session_stats.error` in `UsageRow`.

**Interfaces:** Consume `store.UsageRow` and `store.ModelPrice`. Add `Error string` to `store.UsageRow`. Produce `Tokens`, `Totals`, `ModelSummary`, `TaskSummary`, `SessionSummary`, `Period(rows []store.UsageRow, prices []store.ModelPrice) PeriodSummary`, and `Task(rows []store.UsageRow, prices []store.ModelPrice, taskID int64, prURLForRepo func(string, int) string) TaskUsageSummary` with JSON fields from spec §3. The API passes a repo-to-PR-URL function to keep filesystem lookup out of aggregation.

- [ ] **Step 1: Write failing tests** for billable, per-model price calculation, missing relevant rates, zero-token pricing, partial aggregate cost, session deduplication across models, task grouping including `task_id:null`, deterministic billable-descending order, task-session metadata/duration, uncollected session shapes without inflating totals, and error propagation from stored stats.
- [ ] **Step 2: Run** `go test ./internal/usage` and confirm feature tests fail because interfaces are absent.
- [ ] **Step 3: Implement** the minimal pure aggregation, preserving empty arrays as `[]`, nullable costs as JSON `null`, and nullable task/subtask IDs as `null`.
- [ ] **Step 4: Run** `go test ./internal/usage` and confirm green.
- [ ] **Step 5: Commit** the aggregate and tests.

### Task 2: Usage read endpoints and date window

**Files:** Create `internal/api/usage.go`, `internal/api/usage_test.go`; modify `internal/api/server.go`.

**Interfaces:** Register `GET /v1/stats/usage` and `GET /v1/tasks/{id}/usage`. Use Task 1 aggregate types. Query `Store.UsageRows`, `CountPendingUsage`, `TaskUsageRows`, `ListModelPrices`, `GetTask`, and `GetRepo`; parse GitHub origin via `github.ParseRemote` when building `pr_url`.

- [ ] **Step 1: Write failing API tests** for 30-day default, local inclusive `to` (including daylight-saving date), invalid/reversed dates, project filter, pending count, empty arrays, task rows including running/missing sessions, root-only validation, and absent task.
- [ ] **Step 2: Run** `go test ./internal/api -run 'TestUsage'` and confirm expected missing-route failures.
- [ ] **Step 3: Implement** date parsing, read handlers, root task checks, repo PR URL resolution, and route registration. Return `400 bad_request`, `400 not_root_task`, and `404 task_not_found` as specified.
- [ ] **Step 4: Run** the focused API tests and confirm green.
- [ ] **Step 5: Commit** the read API and tests.

### Task 3: Model prices endpoints

**Files:** Modify `internal/api/usage.go`, `internal/api/usage_test.go`.

**Interfaces:** Register `GET /v1/stats/prices`, `PUT /v1/stats/prices/{model}`, and `DELETE /v1/stats/prices/{model}` using `ListModelPrices`, `UsageModels`, `UpsertModelPrice`, and `DeleteModelPrice`. GET wraps rows as `{ "prices": [...] }`; unpriced rows have null rates and `updated_at`. PUT returns the saved price object, DELETE returns 204.

- [ ] **Step 1: Write failing API tests** for priced and observed unpriced models, exact model IDs, successful PUT/DELETE, negative and nonfinite values, required fields, malformed/trailing JSON, and agent-session `403 human_only` on both mutations.
- [ ] **Step 2: Run** the focused tests and confirm expected failures.
- [ ] **Step 3: Implement** price listing and strict mutation validation; allow `null` rates and zero, reject malformed input, reuse `requireHuman`.
- [ ] **Step 4: Run** the focused tests and confirm green.
- [ ] **Step 5: Commit** the price API and tests.

### Task 4: CLI and public documentation

**Files:** Modify `internal/cli/stats.go`, `internal/cli/stats_test.go`, `docs/03-daemon-api.md`, `docs/04-cli.md`.

**Interfaces:** Add `rocket stats usage [--from D] [--to D] [--project P]`, `rocket stats task <id>`, `rocket stats prices [set <model> --input X --cache-write X --cache-read X --output X | rm <model>]`; support inherited `--json` and use `apiPath` for model IDs.

- [ ] **Step 1: Write failing CLI tests** for command tree, argument/flag errors, safe model path escaping, and table renderers for full and partial cost, empty results, task sessions, and prices.
- [ ] **Step 2: Run** `go test ./internal/cli -run 'TestStatsUsage|TestStatsTask|TestStatsPrices'` and confirm expected failures.
- [ ] **Step 3: Implement** commands and renderers using the existing client and Cobra patterns; document API fields/errors and CLI forms.
- [ ] **Step 4: Run** focused CLI tests and confirm green.
- [ ] **Step 5: Commit** CLI, tests, and docs.

### Task 5: Final verification and PR

- [ ] **Step 1: Run** `gofmt` on changed Go files and `go test ./...`; inspect failures with `superpowers:systematic-debugging`.
- [ ] **Step 2: Exercise** the read, prices PUT/GET/DELETE, and CLI flows end-to-end against an isolated daemon/test store; verify JSON shapes and human-only rejection.
- [ ] **Step 3: Review** diff against W4/spec, check `git status`, push this branch, and open one PR referencing feature `agent-task-stats` and task #5138.
- [ ] **Step 4: Report** PR URL to orchestrator; monitor CI/review and fix until green and approved.
