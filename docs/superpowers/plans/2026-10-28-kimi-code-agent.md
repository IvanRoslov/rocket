# Kimi Code agent — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement your task. Each task below is one rocket worker = one branch `feature/kimi-code-support/<short-name>` = one PR. Write your own step-level TDD plan for your task inside your worktree before coding.

**Goal:** Add a third agent adapter, `kimi-code`, so rocket can run workers and orchestrators on the human's Kimi Code subscription with Codex-level parity.

**Architecture:** New package `internal/agent/kimicode` modelled on `internal/agent/codex`, plus one new optional interface (`agent.FirstMessageByInput`) honoured by the session manager, a Kimi pane parser in `internal/runtime`, and agent-name plumbing in CLI/API/web/docs.

**Tech Stack:** Go (daemon, CLI), TypeScript/React (web dashboard), tmux.

**Spec:** `docs/superpowers/specs/2026-10-28-kimi-code-agent-design.md` (task #5757 spec v2). Recon facts and fixtures: `docs/superpowers/kimi-recon-report.md`, `internal/agent/kimicode/testdata/`, `internal/runtime/testdata/kimi-*.pane` (merged in PR #142) — every worker reads the report too.. Every worker reads it in full.

## Global Constraints

- Agent name (registry key, profiles, API, UI): `kimi-code`. Go package: `internal/agent/kimicode`.
- Kimi Code CLI 2.4.0; binary lookup: `kimi` in PATH → `$KIMI_CODE_HOME/bin/kimi` → `~/.kimi-code/bin/kimi`; LaunchCommand uses the resolved absolute path.
- Kimi home: `$KIMI_CODE_HOME`, default `~/.kimi-code`. Never write to Kimi's global files (`config.toml`, `tui.toml`, hooks, plugins). Read-only access to them is fine.
- Launch: `<kimi> --auto [-m <model>]`. System prompt: rocket block in `<worktree>/AGENTS.md`, same mechanism as codex (`--agent-file` is ignored by the 2.4.0 TUI — do not use it).
- Text injection into a kimi-code pane: bracketed paste (`tmux paste-buffer -p`) then Enter; non-bracketed paste trips Kimi's paste-burst heuristic (Enter → newline).
- Env always: `KIMI_CODE_TRUST_WORKSPACE=1`, `KIMI_CODE_NO_AUTO_UPDATE=1`, `KIMI_DISABLE_TELEMETRY=1`; `KIMI_MODEL_THINKING_EFFORT=<effort>` only when effort set.
- `Efforts()` = `low, high, max`.
- Skills sections (`{{#if skills}}`) are NOT stripped for kimi-code.
- Import profile name: `kimi-` + slug of id without `kimi-code/` prefix, no doubled `kimi-` (`kimi-code/k3.1-pro` → `kimi-k3-1-pro`, `kimi-code/kimi-for-coding` → `kimi-for-coding`).
- Transcript readers: read from the end in 64 KiB blocks where scanning for a recent record; unlimited line length; skip broken lines.
- Live probes may use the human's Kimi subscription, but keep them tiny (one-line prompts) and never in the human's real repos — use a throwaway dir under the worker's scratch/worktree.
- `make test` green; no change to existing claude-code/codex behaviour (their tests untouched and passing).

## Review Focus

1. Kimi creates its session journal only after the first prompt (confirmed by recon) → before the brief is injected, Activity must return `ErrNoSignal` (not `ready`/`idle`), and first-message delivery must rely on the pane parser. Test owner: Task 3 (activity) and Task 2 (manager queues first message).
2. A previous Kimi session in the same worktree (after `restore`) must not be taken as the current one: journal selection respects `ActivityRef.Since`. Test owner: Task 2 (`wire.go` locator: `state.json.createdAt` vs `Since`).
3. Messages typed while Kimi is busy are queued by Kimi and recorded only when their turn starts, as a separate user-origin `turn.prompt` (fixture `queued-second-message.wire.jsonl`); skill-activation `turn.steer` records must not count as delivery. Test owner: Task 3.
4. Quota exhaustion (429) → `turn.ended reason=failed` with error → `blocked`, error text preserved. Test owner: Task 3.
5. `kimi` not in the daemon's PATH but present at `~/.kimi-code/bin/kimi` → spawn works. Test owner: Task 2.

---

### Task 1: Live recon spike — pin Kimi facts and capture fixtures (`kimi-recon`) — DONE, PR #142

**Files:**
- Create: `docs/superpowers/kimi-recon-report.md` (facts, each with how it was observed)
- Create: `internal/agent/kimicode/testdata/` — real `wire.jsonl` samples (redact nothing sensitive is expected; strip any tokens if seen), `session_index.jsonl` sample, `config.toml` models excerpt (no credentials)
- Create: `internal/runtime/testdata/kimi-*.pane` — `capture-pane` snapshots

**Interfaces:** Produces the fixtures and facts Tasks 2–4 build on. No Go code.

- [ ] Run Kimi in tmux in a throwaway git dir with the Global Constraints env and flags; capture: idle empty composer, composer with a draft, busy with a queued message, finished turn.
- [ ] Without `KIMI_CODE_TRUST_WORKSPACE`, in a fresh dir: capture the trust screen pane. Try to capture any other start screens (survey/upgrade); if not reproducible, note source locations of their layouts.
- [ ] Verify `--agent-file` with `${base_prompt}`: required frontmatter/format, that the appended prompt is in effect (ask the model to echo a marker), and whether it conflicts with `--auto`. Decide agent-file vs AGENTS.md; record the decision.
- [ ] Verify `KIMI_MODEL_THINKING_EFFORT=low|high|max` takes effect on `kimi-code/k3.1-pro` (journal `llm.request` / usage or visible TUI indicator).
- [ ] Record journal facts: when the session dir and `wire.jsonl` appear (before/after first prompt), exact shapes of `turn.prompt`, `turn.ended`, `context.append_message`, `usage.record`, how a message queued during a turn is recorded, whether `session_index.jsonl` exists at `sessions/` and its `workDir` format (symlinks resolved?).
- [ ] Verify bracketed paste of a multi-line ~5 KB text via `tmux paste-buffer -p` + Enter submits as one message.
- [ ] Verify project skills at `<gitroot>/.kimi-code/skills/<name>/SKILL.md` are discovered alongside the superpowers plugin.
- [ ] Commit; PR with the report and fixtures.

### Task 2: Adapter core, journal locator, first message by input (`kimi-adapter-core`)

Depends on Task 1 merged.

**Files:**
- Create: `internal/agent/kimicode/kimicode.go`, `kimicode_test.go`, `wire.go`, `wire_test.go`, `catalog.go`, `catalog_test.go`, `effort_test.go`, `skill_test.go`
- Modify: `internal/agent/agent.go` (new interfaces `FirstMessageByInput`, `BracketedPaster`), `internal/session/manager.go` (first message via queue for start/spawn/restore), its tests
- Modify: `internal/cli/doctor.go` (blank import registering `kimi-code`) and wherever else adapters are blank-imported (grep `agent/codex"`)

**Interfaces:**
- Produces in `internal/agent`:
  ```go
  // FirstMessageByInput is implemented by agents whose CLI cannot take the first
  // message on its command line; the session manager queues LaunchSpec.FirstMessage
  // as the session's first delivery instead.
  type FirstMessageByInput interface{ FirstMessageByInput() bool }
  func TakesFirstMessageByInput(a Agent) bool
  // BracketedPaster is implemented by agents whose TUI must receive injected text
  // as a bracketed paste (Kimi: a fast non-bracketed paste turns the next Enter into a newline).
  type BracketedPaster interface{ BracketedPaste() bool }
  func WantsBracketedPaste(a Agent) bool
  ```
- Produces in `kimicode`:
  ```go
  func New() agent.Agent              // registered as "kimi-code"
  func kimiHome() string               // $KIMI_CODE_HOME or ~/.kimi-code
  func resolveBinary() (string, error)
  // findSession scans <home>/sessions/wd_<basename(worktree)>_*/session_*/state.json
  // (falling back to all wd_* dirs), picks the newest session whose state.json "cwd"
  // equals EvalSymlinks(worktree) and whose "createdAt" (unix ms) >= since, and returns
  // its directory; agent.ErrNoSignal if none. Never runs kimi.
  func findSession(home, worktree string, since time.Time) (sessionDir string, err error)
  func mainWire(sessionDir string) string        // <sessionDir>/agents/main/wire.jsonl
  func agentWires(sessionDir string) []string     // all <sessionDir>/agents/*/wire.jsonl
  type wireRecord struct { Type string; Time time.Time; Raw json.RawMessage }
  func readTail(path string, stop func(wireRecord) bool) ([]wireRecord, error) // newest first
  ```
  `Activity`, `TranscriptTail`, `TranscriptStat` are stubs returning `agent.ErrNoSignal` (Tasks 3–4 replace them). Implements `BrainstormSkillShipper` (true), `FirstMessageByInput` (true), `BracketedPaster` (true).
- [ ] Tests: `LaunchCommand` exact argv for (no model/effort), (model), and that FirstMessage never appears in argv; `Env` contains the three fixed vars and the effort var only when set; `Available`/`resolveBinary` with PATH empty and fake `~/.kimi-code/bin/kimi` (HOME in temp dir); `SetupWorkspace` upserts the rocket block into `AGENTS.md` exactly like codex's `upsertAgentsMD` but WITHOUT `prompts.StripSkills` (skills sections kept), git-excluded when untracked, idempotent, foreign content preserved (reuse/extract codex's helper rather than copy it); orchestrator with `BrainstormSkill="orchestrator-brainstorming"` gets `.kimi-code/skills/orchestrator-brainstorming/` (scripts executable, git-excluded), without it the dir is removed; `Catalog` from fixture config (6 models, efforts, defaults), broken TOML and missing file → builtin + warning; `findSession` on a fake home built from fixtures (`state.json`): picks newest matching `cwd`, symlinked worktree path matches physical `cwd`, other cwds ignored, `createdAt < since` ignored, missing basename prefix falls back to all `wd_*`, no sessions → `ErrNoSignal`.
- [ ] Session manager tests: for an agent with `FirstMessageByInput`, Start/Spawn/Restore put FirstMessage as the first queued message (sender `rocket`) and LaunchSpec passed to LaunchCommand has empty FirstMessage; other agents unchanged.
- [ ] Implement, `make test`, commit, PR.

### Task 3: Activity, delivery, pane parser, start screens (`kimi-activity`)

Depends on Task 2 merged.

**Files:**
- Create: `internal/agent/kimicode/activity.go`, `activity_test.go`, `confirm.go`, `confirm_test.go`, `startup.go`, `startup_test.go`
- Create/Modify: `internal/runtime/` Kimi composer parser (new file `kimipane.go` + test), Kimi start-screen parser (new file `kimistartscreen.go` + test) used by the monitor for `kimi-code` sessions; `runtime.InjectOpts.Bracketed bool` → `paste-buffer -p`; queue sets it via `agent.WantsBracketedPaste`; draft guard and readiness use the Kimi parser for `kimi-code` sessions (follow how Codex's composer detection is selected); start-screen answer for unnumbered options = arrow keys to the chosen option + Enter, re-reading the screen first (409 `prompt_changed` if it changed).

**Interfaces:**
- Consumes: `findSession`, `readTail`, `wireRecord` (Task 2).
- Produces: `Activity` (real), `ConfirmDelivered`, `AcceptsInputWhileBusy() bool { return true }`, `HasStartedTurn` (user-origin `turn.prompt` at/after `Since`); runtime: `ParseKimiComposer(pane string) (state KimiComposer, ok bool)` with states `Empty`, `Draft`, `Busy`; `ParseKimiStartScreen(pane string) (PermissionPrompt, bool)`.
- [ ] Activity tests on fixtures: open `turn.prompt` + old mtime → `active`; `turn.ended completed` + old mtime → `ready`; `turn.ended failed` with 429 error → `blocked` with error text; open `interaction.request` → `blocked`; no journal → `ErrNoSignal`.
- [ ] Confirm tests: exact match after `since` on user-origin `turn.prompt`; whitespace-normalized; record before `since` not matched; queued message confirmed by the later `turn.prompt` (fixture `queued-second-message.wire.jsonl`), not by `turn.steer` with `origin.kind=skill_activation`; large body matched by inbox-file reference text; long lines.
- [ ] Pane tests on recon snapshots: `kimi-idle.pane` → Empty, `kimi-draft.pane` → Draft, `kimi-busy-queued.pane` → Busy, `kimi-finished.pane` → Empty; negatives — Claude and Codex panes. Start screen: `kimi-trust.pane` → title `Trust this folder?`, options `Trust and continue`, `Exit`; `kimi-idle.pane`/`kimi-question.pane` handling documented (question = blocked via journal interaction, not a start screen); existing Codex snapshots still pass.
- [ ] Runtime test: `Inject` with `Bracketed: true` issues `paste-buffer -p`; without it the argv is unchanged.
- [ ] Queue test: first message for a kimi-code session waits until the composer is `Empty`, then injects.
- [ ] Implement, `make test`, commit, PR.

### Task 4: Usage and chat transcript (`kimi-usage-chat`)

Depends on Task 2 merged; parallel with Task 3.

**Files:**
- Create: `internal/agent/kimicode/usage.go`, `usage_test.go`, `chat.go`, `chat_test.go`

**Interfaces:**
- Consumes: `findSession`, `wireRecord` (Task 2).
- Produces: `Usage(ctx, worktreePath, since) (agent.Usage, error)`, real `TranscriptTail`, `TranscriptStat`.
- [ ] Usage tests: per-model sums with mapping `inputOther→Input`, `inputCacheRead→CacheRead`, `inputCacheCreation→CacheWrite`, `output→Output`, `Messages` = record count; records before `since` excluded; several sessions in one worktree summed; `Found=false` without journal; sub-agent journals (`agents/agent-N/wire.jsonl`, fixtures `subagent-parent/child`) included and `subagent.completed.usage` NOT added (total equals parent records + child records exactly once).
- [ ] Chat tests on `completed-tool-turn.wire.jsonl`: user and assistant text entries from `agent.message.appended` (`context.append_message` alone would show only user messages), a `Bash` tool entry with ≤120-rune digest of `sleep 12 && printf tool-ok`, TS from record time, cursor resume returns only new entries, `TranscriptStat` mtime/size of `agents/main/wire.jsonl`.
- [ ] Implement, `make test`, commit, PR.

### Task 5: CLI/API/doctor/web/docs surface (`kimi-surface`)

Depends on Task 2 merged; parallel with Tasks 3–4.

**Files:**
- Modify: `internal/cli/models_catalog.go` (source label «из конфига Kimi Code»), `internal/api/model_catalog.go` (`importProfileName` rule from Global Constraints), `internal/cli/doctor.go` (checks below), web: `web/src/lib/types.ts`, `web/src/lib/catalog.ts`, `web/src/screens/system/SystemScreen.tsx`, `web/src/screens/task/SessionRail.css` and related tests/mocks (grep `codex` in `web/src`), mobile if it enumerates agents; docs `docs/10-agents.md` (new section «Адаптер kimi-code», Kimi in «Каталог моделей», «Расход токенов», import naming), other docs listing agents.
- [ ] Tests: `importProfileName("kimi-code","kimi-code/k3.1-pro") == "kimi-k3-1-pro"`, `("kimi-code","kimi-code/kimi-for-coding") == "kimi-for-coding"`; source label; doctor: binary found/missing, logged in (`managed:kimi-code` provider in config + non-empty `credentials/`) vs not, superpowers plugin enabled vs missing/disabled (fixture `plugins/installed.json`) — warnings, not failures; web tests for the new agent label.
- [ ] Implement, `make test` (Go + web), commit, PR.

### Task 6: Live acceptance (orchestrator-run, no PR unless fixes needed)

Depends on Tasks 2–5 merged and the new rocket build deployed; human creates/enables a Kimi profile and allows it for a test task.

- [ ] Spawn a worker on a Kimi profile with a trivial brief (e.g. doc typo fix) → brief delivered, status transitions, `rocket send` mid-turn confirmed, usage visible, chat visible, PR opened.
- [ ] Start a test task with a Kimi orchestrator profile → brainstorm skill present, storm question filed via `rocket task ask`, gate requested.
- [ ] Any defect → systematic-debugging, fix via a new worker task.
