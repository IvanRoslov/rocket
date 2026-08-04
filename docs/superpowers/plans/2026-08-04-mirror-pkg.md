# internal/mirror Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** New package `internal/mirror` for task #795: `Sync` keeps a shared mirror in `~/.rocket/repos/<id>/` fresh (`git fetch origin --prune` + strictly `--ff-only` advance of the *working tree*), `Check` reports freshness purely locally, with no network.

**Architecture:** Two exported functions over `os/exec` git calls in the style of `internal/workspace.runGit` (`exec.CommandContext`, explicit `-C <path>`, no shell). `Sync` is the only thing that touches the network or the working tree; it refuses to advance a mirror that is dirty, off the default branch, or not fast-forwardable, logging via `slog.Warn` and returning nil. `Check` runs the same three guards read-only and surfaces the reason in `Freshness.Blocked`, so a stale mirror can never be read silently.

**Tech Stack:** Go 1.25, stdlib only (`os/exec`, `context`, `log/slog`, `time`) plus `internal/store` for `store.Repo`. Tests use real temp git repos (pattern: `internal/ghpoller/poller_test.go`, `internal/workspace/workspace_test.go`).

## Global Constraints

- **Contractual API** (later tasks consume these exact names — do not rename):
  `Freshness{RepoID, BehindCommits, LastFetch, Blocked, Stale}`,
  `Sync(ctx, repo store.Repo) error`,
  `Check(ctx, repo store.Repo, staleAfter time.Duration, now time.Time) (Freshness, error)`.
- **Never clobber** (CTO hard requirement): the fast-forward is strictly
  `merge --ff-only`. No `reset --hard`, no `checkout -f`, no `clean`. Not
  overwriting is not enough — the reason must be observable via `Blocked`.
- `Check` makes **no** network calls, ever.
- Human-facing strings are Russian (`internal/cli/verifymerge.go` precedent).
- No new module dependencies: stdlib + `internal/store`.
- One mirror's error never panics or blocks others: return/log, never `os.Exit`.

---

## File Structure

- Create `internal/mirror/mirror.go` — package doc, `Freshness`, `Blocked*`
  constants + `BlockedNotOnDefault(branch)`, `Sync`, `Check`, `runGit` helper.
- Create `internal/mirror/mirror_test.go` — temp-git-repo tests for both.

---

### Task 1: `Check` — local, network-free freshness

**Files:**
- Create: `internal/mirror/mirror.go`
- Test: `internal/mirror/mirror_test.go`

**Interfaces:**
- Consumes: `store.Repo{ID, Path, DefaultBranch}` (`internal/store/repos.go:13`).
- Produces:

```go
type Freshness struct {
    RepoID        string
    BehindCommits int       // git rev-list --count HEAD..origin/<default>
    LastFetch     time.Time // mtime of .git/FETCH_HEAD; zero if absent
    Blocked       string    // why Sync cannot advance the tree; "" when fine
    Stale         bool
}

const (
    BlockedDirty = "локальные изменения в зеркале"
    BlockedNoFF  = "fast-forward невозможен"
)

func BlockedNotOnDefault(defaultBranch string) string // "HEAD не на ветке <default>"

func Check(ctx context.Context, repo store.Repo, staleAfter time.Duration, now time.Time) (Freshness, error)
```

`Stale = BehindCommits > 0 || Blocked != "" || LastFetch.IsZero() || now.Sub(LastFetch) > staleAfter`.

Guard precedence for `Blocked`: dirty (`status --porcelain` non-empty) →
off-default (`symbolic-ref --short HEAD` != `DefaultBranch`, including a
detached HEAD where the command fails) → no-ff (`merge-base --is-ancestor HEAD
origin/<default>` exits non-zero).

- [ ] **Step 1: Write the failing tests** (acceptance criteria 2-6): behind-count equals commits pushed to origin; missing `FETCH_HEAD` → zero `LastFetch` and `Stale`; dirty tree → `Blocked == BlockedDirty`, `Stale`; non-default branch → `Blocked` mentions HEAD; freshly synced → `Stale == false`, `BehindCommits == 0`, `Blocked == ""`; `staleAfter` exceeded by an old `LastFetch` → `Stale`.
- [ ] **Step 2: Run `go test ./internal/mirror/... -v`** — expect a compile failure (package does not exist).
- [ ] **Step 3: Implement** `Freshness`, the `Blocked*` constants/helper, `runGit`, `Check`.
- [ ] **Step 4: Run `go test ./internal/mirror/... -run Check -v`** — expect PASS.
- [ ] **Step 5: Commit** `feat(mirror): Check reports mirror freshness without network`.

---

### Task 2: `Sync` — fetch --prune + strictly ff-only tree advance

**Files:**
- Modify: `internal/mirror/mirror.go`
- Test: `internal/mirror/mirror_test.go`

**Interfaces:**
- Consumes: `runGit`, the `Blocked*` vocabulary from Task 1.
- Produces: `func Sync(ctx context.Context, repo store.Repo) error`.

Order (from the brief, order matters):

1. `git -C <path> fetch origin --prune`. Failure is **not** fatal: `slog.Warn`,
   remember the error, continue to step 4 with the refs already present
   (offline must degrade, not break). Return that fetch error to the caller
   *after* the FF attempt.
2. `git status --porcelain` non-empty → `slog.Warn`, return nil, change nothing.
3. `git symbolic-ref --short HEAD` != `repo.DefaultBranch` → `slog.Warn`,
   return nil, change nothing.
4. `git merge --ff-only origin/<repo.DefaultBranch>` → on error `slog.Warn`,
   return nil, change nothing.

- [ ] **Step 1: Write the failing tests** (acceptance criteria 1-3): commit lands in origin → `Sync` → the mirror's *working-tree file* has the new content (incident-#746 regression); dirty mirror → file untouched, commit unchanged, and `Check` then reports `BlockedDirty`/`Stale`; mirror on a non-default branch → tree untouched, `Blocked` mentions HEAD; local divergent commit → not fast-forwarded, commit preserved; unreachable origin URL → error returned, nothing clobbered.
- [ ] **Step 2: Run `go test ./internal/mirror/... -run Sync -v`** — expect FAIL (`undefined: mirror.Sync`).
- [ ] **Step 3: Implement** `Sync`.
- [ ] **Step 4: Run `go test ./internal/mirror/... -v`** — expect PASS.
- [ ] **Step 5: Commit** `feat(mirror): Sync fast-forwards mirrors, never clobbering`.

---

### Task 3: Verification and PR

- [ ] **Step 1:** `go build ./... && go vet ./... && go test ./internal/mirror/... -v` — green, output shown.
- [ ] **Step 2:** `gofmt -l internal/mirror` — empty.
- [ ] **Step 3:** Open the PR from `feature/rocket-repos-4-2/mirror-pkg` referencing feature `rocket-repos-4-2` and task #912; report the URL to `rocket-repos-4-2-orch`.
