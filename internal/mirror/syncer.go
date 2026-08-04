package mirror

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Syncer keeps every registered repo's mirror fresh in the background: once
// per cfg.MirrorSyncInterval it runs Sync over all of them, so an agent
// reading ~/.rocket/repos/<id> sees roughly-current file content instead of
// whatever the mirror happened to be cloned at.
//
// Sync itself never clobbers: a mirror that is dirty, off its default branch,
// or not fast-forwardable is left alone (and stays observable as stale through
// Check). That is what makes an unattended sweep safe.
type Syncer struct {
	st  *store.Store
	cfg *config.Config
}

// NewSyncer builds a Syncer driven by cfg.MirrorSyncInterval. An interval of
// zero (or less) disables background syncing entirely — Run returns
// immediately.
func NewSyncer(st *store.Store, cfg *config.Config) *Syncer {
	return &Syncer{st: st, cfg: cfg}
}

// Run syncs all mirrors every cfg.MirrorSyncInterval, blocking until ctx is
// cancelled. It performs one immediate pass before the first tick, so a
// freshly started daemon does not serve week-old mirrors for a whole interval.
// With a non-positive interval it logs once and returns without syncing.
func (s *Syncer) Run(ctx context.Context) {
	interval := s.cfg.MirrorSyncInterval
	if interval <= 0 {
		slog.Info("mirror: background sync disabled", "mirror_sync_interval", interval)
		return
	}

	s.SyncOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

// isMirror reports whether path is one of the daemon's own clones under
// reposDir, i.e. a mirror this sweep is allowed to advance.
//
// The registry holds two kinds of entry. Clones the daemon made under
// cfg.ReposDir are mirrors and ours to keep fresh. But `rocket repo add
// <path>` registers a human's own working copy, and docs/05-state.md promises
// rocket changes nothing inside it — a clean checkout sitting on main would
// otherwise be silently fast-forwarded under its owner every interval. So
// anything outside reposDir is left strictly alone.
//
// An empty reposDir matches nothing: with no mirror root configured there is
// no entry we can claim as ours.
func isMirror(path, reposDir string) bool {
	if reposDir == "" {
		return false
	}
	rel, err := filepath.Rel(reposDir, resolvePath(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvePath makes a path comparable: absolute, cleaned, and with symlinks
// resolved where possible — on macOS the same directory is reachable as both
// /tmp/x and /private/tmp/x, and a mirror must not be skipped over that.
// Paths that cannot be resolved (a repo whose directory is gone) fall back to
// the cleaned absolute form.
func resolvePath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// SyncOnce runs one pass: Sync over every registered repo. Nothing here is
// fatal — a repo that fails to sync is logged and the pass moves on, because
// one broken mirror must not stop the others from being refreshed.
func (s *Syncer) SyncOnce(ctx context.Context) {
	repos, err := s.st.ListRepos()
	if err != nil {
		slog.Error("mirror: list repos", "error", err)
		return
	}

	reposDir := resolvePath(s.cfg.ReposDir)

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		if !isMirror(repo.Path, reposDir) {
			slog.Debug("mirror: skipping repo outside repos_dir",
				"repo", repo.ID, "path", repo.Path, "repos_dir", s.cfg.ReposDir)
			continue
		}
		if err := Sync(ctx, repo); err != nil {
			slog.Warn("mirror: sync failed", "repo", repo.ID, "path", repo.Path, "error", err)
		}
	}
}
