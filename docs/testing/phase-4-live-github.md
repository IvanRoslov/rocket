# Phase 4 live GitHub testing guide

This is the manual acceptance checklist for the GitHub integration
(auth, repo catalog/clone, PR/CI polling, reactions, auto-cleanup) against
the **real** GitHub API. Phase 4's automated E2E already proves the same
flow against a local stub server (see `.superpowers/sdd/task-8-report.md`);
this document is for validating it against a real repo before relying on it
day to day.

## Prerequisites

- A GitHub account with a real repository you can push branches and open
  PRs against (a scratch/throwaway repo is fine — rocket never writes to
  GitHub itself, but your worker agent will, via `git push` / `gh pr create`).
- A GitHub Personal Access Token (classic or fine-grained) with the `repo`
  scope (fine-grained: Contents read/write, Pull requests read/write,
  Checks read). rocket only *reads* PR/check/review state itself; the
  `repo` scope is what your worker's own `git push` / `gh pr create` calls
  need.
- `rocket` and `rocketd` built and on your `PATH` (or invoked via
  `./rocket` from the repo root).
- A CI workflow on the test repo that actually reports check-run status
  (e.g. a trivial GitHub Actions workflow) — without one, `ci_state` will
  always read `passing` (no check runs = vacuously passing) and the CI
  reaction can't be exercised.

## Token permissions

GitHub Personal Access Tokens (PAT) must have the following minimum scopes to function correctly:

- **Classic PAT**: `repo` scope (full private repository access)
- **Fine-grained PAT**: Repository permissions (minimum required):
  - **Contents: Read** (REQUIRED — core repository access)
  - **Pull requests: Read** (REQUIRED — PR discovery and tracking; missing causes hard failure on `/pulls` endpoint)

Optional permissions (degrade gracefully if absent):
  - **Checks: Read** (optional with degradation — CI state will display as "-", CI nudges disabled, one-time warning in daemon log)
  - **Pull request reviews: Read** (optional with degradation — `ReviewDecision` field unavailable, review-triggered reactions do not fire)

Without `Contents:Read` or `Pull requests:Read`, rocket cannot discover or track PRs — these are REQUIRED permissions.
Without `Checks:Read`, CI state polling is disabled but PR tracking continues.
Without `Pull request reviews:Read`, the review decision is empty and review nudges do not fire.

## Setup

1. **Start the daemon** (first run creates `~/.rocket/config.yaml` with
   defaults — `github_api_base: https://api.github.com` is already correct
   for live use, no edits needed):

   ```
   rocket daemon start
   ```

2. **Authenticate**:

   ```
   rocket github auth <your-PAT>
   ```

   Expect `authenticated as <your-github-login>`. This calls `GET /user`
   against the real API and stores the token in `~/.rocket/rocket.db`
   (`settings` table, 0600 file). Re-running with a bad token should fail
   with an `invalid_token` style error and leave the previously stored
   token untouched.

3. **Register the repo**:

   ```
   rocket repo add --github <owner>/<name>
   ```

   This clones the repo (`git clone https://github.com/<owner>/<name>.git`,
   authenticated via a short-lived header, not embedded in the URL) into
   `~/.rocket/repos/<owner>__<name>` and registers it. Confirm with
   `rocket repo ls` — `auto_cleanup` should read `true` by default (see
   "Disabling auto-cleanup" below to opt a repo out).

4. **Project + task**:

   ```
   rocket project create demo --main <repo-id>
   rocket task add "Real GitHub PR test" --project demo \
     --desc "Open a small PR against <owner>/<name>, e.g. add a comment to a file, then wait for reactions."
   rocket task start <task-id>
   ```

   `task start` spawns a live orchestrator (tmux + your configured agent,
   default `claude-code`). Attach with `rocket attach <session>` to watch
   it, or just let it run. Have it (or a worker it spawns) create a real
   feature branch, commit, push, and open a PR via `gh pr create` — the
   branch name rocket's worker creates follows `feature/<slug>/<task>`, and
   PR discovery keys off exactly that branch name, so let rocket create the
   worker (`rocket spawn` from within the orchestrator, or let the
   orchestrator do it) rather than pushing a branch by hand.

## What to expect

- **PR discovery**: within one poll interval (`github_poll_interval`,
  default 2m) after the PR is opened, `rocket ls` should show the worker's
  session with a `PR` column populated (`#<n>`) and a `CI` column
  (`pending`/`passing`/`failing`). The subtask should move from
  `in_progress` to `review` (`rocket task show <subtask-id>`, look for a
  `PR #<n> opened → review` log line).
- **CI reactions**: if the PR's checks go red, within one more poll
  interval the worker's terminal (`rocket attach <worker>`) should show an
  incoming message:
  `[rocket] CI failing on PR #<n>: <summary>. Investigate and fix.` — sent
  at most once per (session, head SHA); pushing a new commit to the same
  failing state re-arms it. A `changes_requested` review produces a
  similar nudge about addressing review comments.
- **Merge → auto-cleanup**: once the PR is merged, the subtask moves to
  `done` (`PR #<n> merged → done` log line) essentially immediately. The
  worker's session isn't touched right away — rocket waits `merge_grace`
  (default 5m) to let the worker finish any post-merge cleanup of its own,
  then, if the worker is idle (not `active`) and the repo has
  `auto_cleanup: true`, it:
  - kills the worker's tmux session,
  - destroys its git worktree (the branch itself is **not** deleted — it
    stays in the repo clone),
  - marks the session `done`,
  - publishes a `workspace.cleanup` event.

  If the worker is still actively working when grace expires, rocket
  reschedules the check (bounded retries) rather than killing an active
  worker out from under it — if it stays active long enough, cleanup is
  skipped for that merge and needs a manual `rocket kill <worker>
  --cleanup`.
- **Orchestrators are never auto-cleaned up** by this mechanism — only
  worker sessions tied to a merged PR.

## Disabling auto-cleanup

Auto-cleanup is on by default per repo. To opt a specific repo out (e.g.
you want to inspect merged workers' worktrees by hand), PATCH it — there's
no dedicated CLI flag yet, so go through the daemon's Unix socket directly:

```
curl -s --unix-socket ~/.rocket/rocket.sock \
  -X PATCH http://localhost/v1/repos/<repo-id> \
  -d '{"auto_cleanup": false}'
```

Re-enable the same way with `{"auto_cleanup": true}`. Confirm with
`rocket repo ls --json`.

## Troubleshooting

- **No PR shows up after several poll intervals**: check
  `rocket events --session <worker>` for `pr.opened` — if it never fires,
  confirm the worker's branch name actually matches
  `feature/<slug>/<task>` and that the repo's `git remote get-url origin`
  is a GitHub URL rocket recognizes (`git@github.com:owner/repo.git`,
  `https://github.com/owner/repo.git`, or `ssh://git@github.com/owner/repo.git`
  — a `git remote -v` in the repo's registered path, not the worktree, is
  what the poller actually reads).
- **`rocket github auth` fails**: check the token has the `repo` scope and
  hasn't expired; `rocket logs` (daemon log) will show the underlying HTTP
  status if the API call itself failed rather than just returning
  "invalid".
- **CI reaction never fires despite a red PR**: confirm the repo actually
  has check runs on the head commit (`ci_state` reads `passing` when there
  are zero check runs at all — that's the "no CI configured" case, not a
  bug) and that `github_poll_interval` has actually elapsed —
  `rocket events --session <worker>` should show `pr.ci_changed` events as
  the rollup changes.
- **Merge doesn't trigger cleanup**: verify `repos.auto_cleanup` is `true`
  for the repo (`rocket repo ls --json`), that `merge_grace` has actually
  elapsed, and that the worker was idle (not mid-tool-call) when it did.
  `rocket logs` will show
  `ghpoller: reactions: skipping auto-cleanup, AutoCleanup disabled` or
  `ghpoller: reactions: giving up on merge-grace reschedule, worker still active`
  if either of those is why it didn't happen.
- **General**: `rocket events [--session <id>] [--follow]` is the single
  best window into what the poller/reactions pipeline is doing;
  `rocket logs` (or `~/.rocket/logs/rocketd.log`) has the daemon's
  structured logs including any GitHub API errors (rate limiting shows up
  as a backoff log line, not a crash).
