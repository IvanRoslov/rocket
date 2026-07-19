# Phase 5 live multi-agent testing guide

This is the manual acceptance checklist for running a **codex** worker under
a **claude-code** orchestrator end-to-end (spawn → PR → merge → cleanup)
against the **real** GitHub API. Phase 5's automated E2E already proves the
same flow against a local GitHub stub server; this document is for
validating it live before relying on it day to day. It builds directly on
`docs/testing/phase-4-live-github.md` — read that first for the GitHub auth
/ repo / poller setup; this doc only covers what's specific to a codex
worker.

## Prerequisites

- Everything in `phase-4-live-github.md`'s prerequisites (a scratch GitHub
  repo + PAT with `repo` scope, a CI workflow on it, `rocket`/`rocketd` on
  `PATH`).
- The `codex` CLI installed and **already logged in** (`codex login` once,
  interactively, before running rocket — rocket does not drive codex's own
  auth flow). `rocket doctor` should show `✅ agent:codex: доступен`; if it
  doesn't, fix that before attempting a live run.

## How to repeat the scenario live

1. Follow `phase-4-live-github.md` steps 1–4 (daemon start, `rocket github
   auth`, `rocket repo add --github <owner>/<name>`, project create) exactly
   as written.
2. Start the orchestrator: `rocket task start <task-id>` (or `rocket up
   "<desc>" --project <id>`), default agent `claude-code`.
3. Get a codex worker spawned. In practice the orchestrator, following its
   own brainstorming/planning prompt, may want to scope out a larger plan
   before spawning anything — for a quick smoke test it's faster to tell it
   directly via `rocket send <orch> "..."` to skip planning and run `rocket
   spawn --repo <repo-id> --task <name> --agent codex --prompt '<brief>'`
   verbatim. If the orchestrator still won't cooperate, spawning the worker
   yourself with `ROCKET_SESSION_ID=<orch-id> rocket spawn --agent codex
   ...` (attributing the spawn to the orchestrator via the same header the
   CLI would send) is an accepted fallback — this is what rocket's own
   automated E2E does when driving the scenario non-interactively.
4. Have the codex worker create a branch commit, push, and open a real PR
   (`git push` + `gh pr create`, or let it do so per its own brief) — same
   branch-naming requirement as phase 4 (`feature/<slug>/<task>`).

## What to expect

- Same PR discovery / CI reaction / merge → auto-cleanup behavior as
  documented in `phase-4-live-github.md` — none of that is agent-specific,
  it's driven entirely by the GitHub poller reading PR/check-run state, not
  by which CLI the worker session runs.
- The CI-failing nudge (`[rocket] CI failing on PR #<n>: ...`) is delivered
  into the codex worker's tmux pane the same way it's delivered to a
  claude-code worker (`rocket send`-style injection into stdin); watch with
  `rocket attach <worker>`. Whether/how codex *acts* on it is up to codex,
  not rocket.
- `rocket doctor` should show both `✅ agent:claude-code` and `✅
  agent:codex`.

## Known limitations

- **No push channel for codex.** Unlike some agent CLIs, codex has no
  mechanism for rocket to push messages into an already-open session other
  than tmux stdin injection (which rocket already uses for both agents). All
  of rocket's picture of a codex worker's state — activity (`active` /
  `ready` / `blocked`), whether it's read a nudge, whether it's idle enough
  for merge-grace cleanup — comes from `activity_poll_interval`-cadence
  polling of the codex session's transcript/state, never a push/webhook from
  codex itself. Expect state changes to lag by up to one poll interval,
  more so than an event-driven integration would.
- **Error-state classification is best-effort.** As noted in the phase 4
  task reports, codex's activity classifier has not been exercised against
  a genuine codex-level turn error (only tool-call failures, which codex
  itself absorbs and reports as normal output) — the conservative fallback
  (misclassification defaults to `ready`, never wrongly `blocked`) is
  implemented but not empirically validated end-to-end.
- **Merge-grace cleanup has bounded retries.** If a worker is still `active`
  (e.g. mid-investigation of a CI-failing nudge) every time the poller
  rechecks it during the `merge_grace` window, rocket gives up after a fixed
  number of reschedules rather than retrying forever, and cleanup has to be
  finished manually with `rocket kill <worker> --cleanup`. This is
  documented, intentional behavior (rocket never kills an actively-working
  session out from under it) — it's not specific to codex, but a fast
  `merge_grace`/`github_poll_interval` combined with a worker that's
  genuinely busy right at merge time is enough to trigger it in practice.
