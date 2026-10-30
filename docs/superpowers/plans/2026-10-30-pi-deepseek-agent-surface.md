# Pi and DeepSeek Agent Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the merged Pi and DeepSeek adapters through Rocket's registry, profiles/catalog/import, detection, doctor, web labels/mocks, and documentation without changing existing agent behavior.

**Architecture:** Register the existing adapters at the daemon/CLI composition roots and add only the presentation and detection logic around them. Reuse the current profile schema, catalog cache/import pipeline, doctor result type, and web registry-driven screens; do not modify either adapter.

**Tech Stack:** Go, Cobra, `net/http`, Vitest, React, TypeScript, MSW, Markdown.

**Spec:** `/Users/ivanroslov/.rocket/worktrees/rocket/pi-and-deepseek2-orch/docs/superpowers/specs/2026-10-29-pi-deepseek-agents-design.md` (binding v4); Task 6 requirements in `.rocket/inbox/msg-79098.md` and the orchestrator's `task-6-brief.md`.

## Global Constraints

- Do not edit `internal/agent/pi` or `internal/agent/deepseek`; their production adapters are already merged.
- Preserve the profile schema and every existing agent's launch, environment, restore, and delivery behavior.
- Pi import names use `pi-` plus the full provider/model slug without a duplicated provider; DeepSeek keeps its model id slug. No provider schema, DSH, auth storage, or forced cache TTL.
- `pi` detects as `pi`; an explicit DeepSeek `ANTHROPIC_BASE_URL` before `claude` detects as `deepseek`; bare `claude` stays `claude-code`; wrappers stay unknown.
- Doctor never prints credentials or changes global config. Pi Superpowers is checked only for a usable Pi, through `pi list` with a timeout. DeepSeek uses its generic availability result.
- Web labels and mocks must match actual adapters. Do not add mobile UI; mobile changes require a proven hardcoded adapter list.
- Use red/green TDD, no subagents, no unrelated refactors. Finish with `make test`, `git diff --check`, one PR, and no self-merge.

## Review Focus

- Bare and unrelated `claude` launchers must never be reclassified as DeepSeek; pin the detection matrix in Task 1.
- `deepseek/deepseek-flash` and `openai/gpt-6-sol` must not produce doubled provider prefixes; pin both plus collision suffixes in Task 1.
- Pi doctor must not run when Pi is unusable, leak env/output secrets, or hang; pin those cases in Task 2.
- New registry entries change all-agents catalog/import counts; update those assertions without weakening existing coverage in Task 1.
- Generic UI must show the new agents without a separate mobile path; pin System, SessionRail, StartModal, and Settings in Task 3, and leave mobile unchanged after inspecting its lists.

---

### Task 1: Registry, detection, catalog/import, and CLI labels

**Files:**
- Modify: `internal/daemon/daemon.go`
- Modify: `internal/agent/detect.go`, `internal/agent/detect_test.go`
- Modify: `internal/api/model_catalog.go`, `internal/api/model_catalog_test.go`, `internal/api/main_test.go`, `internal/api/agent_kinds_test.go`
- Modify: `internal/cli/doctor.go`, `internal/cli/models_catalog.go`, `internal/cli/models_catalog_test.go`

**Interfaces:**
- Consumes: `agent.Register` in the merged `pi` and `deepseek` packages; `agent.Catalog`; existing `CatalogCache` and `importProfileName(agentName, modelID string) string`.
- Produces: registered `pi` and `deepseek` kinds in daemon/CLI; `DetectFromCommand(command string) string`; Pi import names such as `pi-deepseek-flash` and `pi-openai-gpt-6-sol`; CLI source label `из Pi CLI`.

- [ ] **Step 1: Write failing detection and import-name tests**

Add table cases asserting:
- `pi`, `/usr/local/bin/pi --approve`, and `env FOO=1 pi` => `pi`.
- `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic claude` and the same after `env` => `deepseek`.
- bare `claude`, `claude` with unrelated `ANTHROPIC_BASE_URL`, and `bash -c 'pi'` retain their old/empty results.
- `importProfileName("pi", "deepseek/deepseek-flash") == "pi-deepseek-flash"`.
- `importProfileName("pi", "openai/gpt-6-sol") == "pi-openai-gpt-6-sol"`.
- `importProfileName("deepseek", "deepseek-flash[1m]") == "deepseek-flash-1m"`.

Add a Pi import collision test: pre-create `pi-deepseek-flash` and `pi-deepseek-flash-2`, import Pi, and expect `pi-deepseek-flash-3` plus `pi-deepseek-v4-pro`.

- [ ] **Step 2: Extend API/CLI expectations before implementation**

Add Pi and DeepSeek fake catalogs to `internal/api/main_test.go` using the adapters' actual ids and effort lists. Update all-agents catalog/import assertions for sorted `claude-code`, `codex`, `deepseek`, `pi`, eight imported profiles, eight second-import skips, and eleven total profiles in the concurrent test. Add agent-kinds assertions for both new agents and Pi efforts `off,minimal,low,medium,high,xhigh,max`. Add CLI assertions for `catalogSourceLabel("pi", "cli") == "из Pi CLI"` and DeepSeek's generic `встроенный список`.

- [ ] **Step 3: Run the affected backend tests and verify RED**

Run: `go test ./internal/agent ./internal/api ./internal/cli -run 'Test.*(Detect|Import|Catalog|AgentKinds)' -count=1`
Expected: FAIL on missing detection/import branches and new agents; do not proceed while failures are unexamined.

- [ ] **Step 4: Implement minimal registration, detection, naming, and labels**

Blank-import both adapters in `internal/daemon/daemon.go` and `internal/cli/doctor.go`; also blank-import them in `agent_kinds_test.go` so the endpoint test covers them. Add `pi` to `commandBinaries`. In `DetectFromCommand`, remember an explicitly parsed `ANTHROPIC_BASE_URL` assignment whose value is exactly `https://api.deepseek.com/anthropic`; when the resulting binary is `claude`, return `deepseek` only for that value. Do not inspect wrapper bodies.

In `importProfileName`, prefix Pi with `pi-`, slug the complete `provider/model`, and remove the provider prefix from the model portion only when it duplicates `provider-`. Keep the existing generic slug/collision functions and DeepSeek's default id path. Add Pi's CLI source case and `pi | deepseek` to the two agent-flag help strings.

- [ ] **Step 5: Run affected tests and verify GREEN**

Run: `go test ./internal/agent ./internal/api ./internal/cli -run 'Test.*(Detect|Import|Catalog|AgentKinds)' -count=1`
Expected: PASS, including all pre-existing catalog, collision, and effort tests.

- [ ] **Step 6: Commit the backend exposure slice**

```bash
git add internal/daemon/daemon.go internal/agent/detect.go internal/agent/detect_test.go internal/api/model_catalog.go internal/api/model_catalog_test.go internal/api/main_test.go internal/api/agent_kinds_test.go internal/cli/doctor.go internal/cli/models_catalog.go internal/cli/models_catalog_test.go
git commit -m "feat(agents): register Pi and DeepSeek catalogs"
```

### Task 2: Pi Superpowers doctor check

**Files:**
- Modify: `internal/cli/doctor.go`
- Create: `internal/cli/doctor_pi.go`, `internal/cli/doctor_pi_test.go`

**Interfaces:**
- Consumes: `agent.Registry()["pi"].Available()` and `checkResult`.
- Produces: `checkPiAgent() []checkResult`, `checkPiSuperpowers() checkResult`; a result named `pi:superpowers` with install hint `pi install git:github.com/obra/superpowers`.

- [ ] **Step 1: Write failing doctor tests**

Stub the package-level Pi list runner to cover:
- output containing `git:github.com/obra/superpowers` => OK;
- missing package => warning containing the exact install hint;
- runner error and context timeout => warning, never failure;
- a secret placed in the environment and runner output/error never appears in `checkResult.Detail`;
- `checkPiAgent` returns no extra result when Pi is unavailable, and exactly the Superpowers result when usable.

Use a temporary executable `pi` fixture for availability integration where needed; make the timeout test wait on `ctx.Done()` rather than sleep.

- [ ] **Step 2: Run doctor tests and verify RED**

Run: `go test ./internal/cli -run 'Test.*(Doctor|Pi)' -count=1`
Expected: FAIL because `doctor_pi.go` and its check are absent.

- [ ] **Step 3: Implement the bounded, read-only check**

Add `checkPiAgent` following the Kimi pattern: no result when Pi is missing or unusable. Add `checkPiSuperpowers` using `exec.CommandContext(ctx, "pi", "list")` with a bounded timeout and `WaitDelay`; keep the runner replaceable for tests. Search only the returned package listing for `git:github.com/obra/superpowers`. Return a warning with the install hint on missing package, error, or deadline. Include only fixed diagnostic text, never raw env or command output. Append `checkPiAgent()` in `runDoctorChecks` after the generic agent checks.

- [ ] **Step 4: Run doctor tests and verify GREEN**

Run: `go test ./internal/cli -run 'Test.*(Doctor|Pi)' -count=1`
Expected: PASS, including generic DeepSeek availability remaining under `checkAgents`.

- [ ] **Step 5: Commit the doctor slice**

```bash
git add internal/cli/doctor.go internal/cli/doctor_pi.go internal/cli/doctor_pi_test.go
git commit -m "feat(doctor): check Pi Superpowers installation"
```

### Task 3: Web titles, source labels, fixtures, and generic screens

**Files:**
- Modify: `web/src/lib/catalog.ts`, `web/src/lib/catalog.test.ts`
- Modify: `web/src/mocks/handlers.ts`, `web/src/mocks/fixtures.ts`
- Modify: `web/src/screens/system/SystemScreen.tsx`, `web/src/screens/system/SystemScreen.test.tsx`
- Modify: `web/src/screens/task/SessionRail.test.tsx`, `web/src/screens/kanban/StartModal.test.tsx`, `web/src/screens/settings/ModelsSection.test.tsx`

**Interfaces:**
- Consumes: actual adapter efforts and catalog ids; existing registry-driven screens.
- Produces: titles `Pi` and `DeepSeek`; `catalogSourceText` values `from Pi CLI` and `built-in`; matching MSW profiles/import behavior.

- [ ] **Step 1: Write failing web tests**

Assert Pi CLI => `from Pi CLI` and DeepSeek builtin => `built-in`. Extend SessionRail workers to include `pi` and `deepseek` cards, StartModal's empty-registry picker to offer both, System's running-unit copy to include both, and Settings source text to name both catalogs. Update the import expectation to include the new models and assert `pi-deepseek-flash` and `deepseek-flash-1m` are created disabled.

- [ ] **Step 2: Run affected Vitest files and verify RED**

Run: `cd web && npm test -- --run src/lib/catalog.test.ts src/screens/system/SystemScreen.test.tsx src/screens/task/SessionRail.test.tsx src/screens/kanban/StartModal.test.tsx src/screens/settings/ModelsSection.test.tsx`
Expected: FAIL on missing labels, agents, and catalog entries.

- [ ] **Step 3: Implement labels and realistic mocks**

Add both titles to `AGENT_TITLE`; builtin handling already returns `built-in`. Set System running unit to `claude/codex/kimi/pi/deepseek`. Add `pi` efforts `off,minimal,low,medium,high,xhigh,max` and DeepSeek efforts `low,medium,high,xhigh,max` to `AGENT_EFFORTS`. Add both agent kinds and their actual catalog models to MSW fixtures. Mirror Pi's provider/model import naming in the mock, including de-duplication, and leave DeepSeek on its normal id slug. Do not change profile shape or screen behavior. Mobile has only generic persistent-agent presentation and no adapter registry list, so leave it untouched.

- [ ] **Step 4: Run affected Vitest files and verify GREEN**

Run the command from Step 2. Expected: PASS, with the expanded import count computed from the two new catalogs and no lost existing assertions.

- [ ] **Step 5: Commit the web slice**

```bash
git add web/src/lib/catalog.ts web/src/lib/catalog.test.ts web/src/mocks/handlers.ts web/src/mocks/fixtures.ts web/src/screens/system/SystemScreen.tsx web/src/screens/system/SystemScreen.test.tsx web/src/screens/task/SessionRail.test.tsx web/src/screens/kanban/StartModal.test.tsx web/src/screens/settings/ModelsSection.test.tsx
git commit -m "feat(web): expose Pi and DeepSeek model profiles"
```

### Task 4: Required documentation

**Files:**
- Modify: `docs/00-overview.md`, `docs/01-concepts.md`, `docs/02-architecture.md`, `docs/04-cli.md`, `docs/10-agents.md`

**Interfaces:**
- Consumes: implemented behavior from Tasks 1–3 and the binding spec.
- Produces: current user/developer documentation for both agents and their non-goals.

- [ ] **Step 1: Update agent lists and architecture references**

List all five adapters in the overview, session agent field, architecture diagram/components, and `docs/10-agents.md` interface/effort summaries. Preserve existing agents' descriptions and make clear that registration is compiled-in.

- [ ] **Step 2: Document Pi behavior and setup**

Document the exact launch flags (`--tui-mode regular --approve --session-id`, optional `--model` and `--thinking`), positional first message, provider/model env selection, native JSONL session/restore/activity/chat/usage, steering acceptance and delivery confirmation, Pi pane/draft behavior, and `AGENTS.md`/`.pi` orchestrator skill setup. Document `pi install git:github.com/obra/superpowers`, the doctor check, catalog/fallback, and Pi import-name examples. State that global Pi settings/auth/packages are not changed and no forced cache retention is set.

- [ ] **Step 3: Document DeepSeek behavior and cache semantics**

Document the official `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, daemon-provided `DEEPSEEK_API_KEY`, model/effort env defaults, `claude` availability requirement, isolated ordinary Claude behavior, built-in catalog, import naming, native session/restore, and actual cache hit/miss usage. State that caching is automatic best effort with no guaranteed hit rate or TTL control.

- [ ] **Step 4: Update CLI/persistent-agent detection and non-goals**

Document `rocket models catalog|import --agent pi|deepseek`, source labels, all five allowed agent values, Pi doctor output, and `pi`/explicit DeepSeek/bare Claude/wrapper detection. Explicitly exclude DSH, provider profile schema, credential storage, proxies, automatic fallback, separate mobile UI, and forced cache TTL.

- [ ] **Step 5: Review and commit docs**

Read the changed sections for consistency with code and run `git diff --check`. Commit:

```bash
git add docs/00-overview.md docs/01-concepts.md docs/02-architecture.md docs/04-cli.md docs/10-agents.md docs/superpowers/plans/2026-10-30-pi-deepseek-agent-surface.md
git commit -m "docs(agents): describe Pi and DeepSeek integrations"
```

### Task 5: Full verification, report, and PR

**Files:**
- Create: `.superpowers/sdd/2026-10-29-pi-deepseek-agents/task-6-report.md`

**Interfaces:**
- Consumes: the complete branch and its tests.
- Produces: verification evidence, one PR from `feature/pi-and-deepseek2/agent-surface`, and a concise orchestrator report.

- [ ] **Step 1: Run the complete suite and static checks**

Run: `make test`
Expected: all Go and web tests PASS.
Run: `git diff --check`
Expected: no output and exit 0.

- [ ] **Step 2: Inspect the delivered diff and repository state**

Run `git status --short`, `git diff --stat origin/main...HEAD`, and `git log --oneline origin/main..HEAD`. Confirm only Task 6 files changed, no adapter internals or mobile files changed, no secrets or generated artifacts are present, and every new test is included. If CI exists, watch it to green; otherwise record the Q11 local-test basis.

- [ ] **Step 3: Write the required report**

Record status, commit ids, PR URL, exact commands/results, CI status, and concerns in `.superpowers/sdd/2026-10-29-pi-deepseek-agents/task-6-report.md`. Explicitly note that this task does not perform live provider acceptance and whether any binary/credentials were unavailable.

- [ ] **Step 4: Open exactly one PR and notify the orchestrator**

Push the branch and run `gh pr create` with title `feat(agents): expose Pi and DeepSeek integrations` and a body covering backend/web/docs changes and Go/web verification. Do not merge. Send only status, commit ids, PR URL, one-line test summary, and concerns to `pi-and-deepseek2-orch`; respond to CI/review until green and approved.
