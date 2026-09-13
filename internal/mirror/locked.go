package mirror

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirrorlock"
	"github.com/IvanRoslov/rocket/internal/store"
)

// This file is the only way a mirror is written. Four callers do it — the
// daemon's Syncer, `rocket repo sync`, `rocket repo sync --repair`, and the
// workspace clone — and until they all went through one lock they raced each
// other over git's index. The first `--repair` run against a live daemon left
// 18 mirrors stranded on rescue/* (the commit landed, the checkout lost the
// race) and 26 abandoned .git/index.lock files (#3589).
//
// The shape is deliberately narrow: take the mirror lock, reap an index.lock
// nobody can still own, do the one thing, record it, release. A caller that
// cannot take the lock gets *mirrorlock.ErrBusy and touches nothing.

// LockOptions is how a caller waits for a mirror and what it considers an
// abandoned index.lock. Every field has a working zero value, so a caller
// with no mirror root still syncs — see ReposDir.
type LockOptions struct {
	// ReposDir is the mirror root: the lock files live under its .locks and
	// the sync sidecars under its .state. Empty means the caller has no
	// mirror root — there is nowhere to put a lock file and nothing to
	// record, so the work runs unlocked. That is the shape tests and a host
	// without repos_dir get, not a silent downgrade of a configured one.
	ReposDir string
	// Timeout bounds the wait for the mirror lock. On expiry the caller gets
	// *mirrorlock.ErrBusy naming the holder, and the mirror is left alone.
	Timeout time.Duration
	// IndexLockMaxAge is how old a .git/index.lock must be before it counts
	// as abandoned. Zero disables reaping.
	IndexLockMaxAge time.Duration
}

// SyncLocked is Sync under the mirror's lock, recording the outcome for
// `rocket repo status`. operation is one of the Op* constants and names the
// holder in both the lock file and the sidecar.
//
// The returned error is *mirrorlock.ErrBusy when the mirror is held by
// somebody else — nothing was touched — and otherwise whatever Sync itself
// reports.
func SyncLocked(ctx context.Context, repo store.Repo, opts LockOptions, operation string) (SyncResult, error) {
	var res SyncResult
	err := withMirrorLock(ctx, repo, opts, operation, func(ctx context.Context, l *mirrorlock.Lock, removed bool) error {
		var syncErr error
		res, syncErr = Sync(ctx, repo)
		res.LockWaited, res.IndexLockRemoved = l.Waited(), removed
		RecordSync(repo.ID, StateDir(opts.ReposDir), operation, res)
		return syncErr
	})
	return res, err
}

// RepairLocked is Repair under the mirror's lock. Repair commits, moves HEAD
// and then syncs — three writes that must not be interleaved with anyone
// else's, which is precisely what went wrong in #3576.
//
// The outcome recorded is Repair's closing Sync, not a sync from before the
// repair: that is what the mirror's state actually is afterwards.
func RepairLocked(ctx context.Context, repo store.Repo, now time.Time, opts LockOptions, operation string) (RepairResult, error) {
	var res RepairResult
	err := withMirrorLock(ctx, repo, opts, operation, func(ctx context.Context, l *mirrorlock.Lock, removed bool) error {
		var repairErr error
		res, repairErr = Repair(ctx, repo, now)
		res.Sync.LockWaited, res.Sync.IndexLockRemoved = l.Waited(), removed
		RecordSync(repo.ID, StateDir(opts.ReposDir), operation, res.Sync)
		return repairErr
	})
	return res, err
}

// WithLock runs fn while holding the mirror's lock. It exists for the one
// writer that is not a sync: the workspace clone runs `git fetch` and `git
// worktree add` inside the mirror, so its lock has to span the clone itself
// and not just the fast-forward before it.
//
// fn's own error comes back unwrapped — a failed `worktree add` is the
// clone's failure, not the lock's.
func WithLock(ctx context.Context, repo store.Repo, opts LockOptions, operation string, fn func(context.Context) error) error {
	return withMirrorLock(ctx, repo, opts, operation, func(ctx context.Context, _ *mirrorlock.Lock, _ bool) error {
		return fn(ctx)
	})
}

// withMirrorLock is the shared body: acquire, reap, run, release. The reaping
// happens with the lock held, which is the entire justification for reaping on
// age alone — no other rocket process can be inside this mirror by then.
func withMirrorLock(ctx context.Context, repo store.Repo, opts LockOptions, operation string,
	fn func(context.Context, *mirrorlock.Lock, bool) error) error {
	if err := validate(repo); err != nil {
		return err
	}
	if opts.ReposDir == "" {
		return fn(ctx, nil, false)
	}

	lock, err := mirrorlock.Acquire(ctx, mirrorlock.LocksDir(opts.ReposDir), repo.ID, operation, opts.Timeout)
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Release(); err != nil {
			slog.Warn("mirror: cannot release the mirror lock", "repo", repo.ID, "error", err)
		}
	}()
	if waited := lock.Waited(); waited > 0 {
		slog.Debug("mirror: waited for the mirror lock", "repo", repo.ID, "operation", operation, "waited", waited)
	}

	removed, err := mirrorlock.ReapIndexLock(gitDirOf(repo.Path), opts.IndexLockMaxAge, time.Now())
	if err != nil {
		// A lock we could not reap is not a reason to skip the sync: git
		// will say so itself, and that error is the one worth reporting.
		slog.Warn("mirror: cannot reap a stale index.lock", "repo", repo.ID, "error", err)
	}

	return fn(ctx, lock, removed)
}

// gitDirOf is where a mirror's index.lock lives. Mirrors are ordinary clones
// the daemon made, so .git is a directory inside the worktree; asking git
// would mean shelling out for a path we already know.
func gitDirOf(path string) string { return filepath.Join(path, ".git") }

// LockBusy reports whether err is a mirror held by another process. Callers
// render that very differently from a git failure — nothing was attempted,
// and the answer is to wait or to find out who is holding it — so they get a
// helper rather than each importing mirrorlock for one errors.As.
func LockBusy(err error) (*mirrorlock.ErrBusy, bool) {
	var busy *mirrorlock.ErrBusy
	if errors.As(err, &busy) {
		return busy, true
	}
	return nil, false
}
