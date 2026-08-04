package mirror

import (
	"context"
	"log/slog"
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

// SyncOnce runs one pass: Sync over every registered repo. Nothing here is
// fatal — a repo that fails to sync is logged and the pass moves on, because
// one broken mirror must not stop the others from being refreshed.
func (s *Syncer) SyncOnce(ctx context.Context) {
	repos, err := s.st.ListRepos()
	if err != nil {
		slog.Error("mirror: list repos", "error", err)
		return
	}

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		if err := Sync(ctx, repo); err != nil {
			slog.Warn("mirror: sync failed", "repo", repo.ID, "path", repo.Path, "error", err)
		}
	}
}
