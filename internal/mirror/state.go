package mirror

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// The last sync result lives in a sidecar JSON file per mirror rather than
// in rocket.db. Two reasons, both about who is holding what: the daemon's
// Syncer writes the result while the CLI's `rocket repo status` prints it —
// different processes — and status must keep working with no daemon running
// at all. A file under the repos dir needs no schema migration and no open
// database handle to answer "why is this mirror still behind".
//
// The file is written whole-then-renamed, so a reader never sees half of it.

// stateDirName is the directory under repos_dir holding the sidecars. The
// leading dot keeps it out of the way of the mirrors themselves, which are
// named after their repo ids.
const stateDirName = ".state"

// SyncState is the sidecar record of the last sync of one mirror: what ran,
// when, and what it could not do. The errors are strings because they are
// read back in a different process — what a human or an agent needs is the
// sentence git produced, not a typed error to branch on.
type SyncState struct {
	RepoID string `json:"repo_id"`
	// At is when the sync ran.
	At time.Time `json:"at"`
	// By names the caller: "syncer" | "repo-sync" | "repo-sync-repair" |
	// "workspace-clone". A mirror stuck behind reads very differently
	// depending on whether the background sweep or a human last touched it.
	By string `json:"by"`
	// FetchErr is a failed `git fetch origin --prune`, empty when it worked.
	FetchErr string `json:"fetch_err,omitempty"`
	// MergeErr is a failed `git merge --ff-only` — the error this whole
	// sidecar exists to make visible.
	MergeErr string `json:"merge_err,omitempty"`
	// Blocked is the mirror package's own refusal to clobber, verbatim.
	// Recorded, but it is not an error: Check reports it independently.
	Blocked string `json:"blocked,omitempty"`
	// IndexLockRemoved records that an abandoned .git/index.lock was reaped
	// before this sync. Filled by the locked entry points (Task 3).
	IndexLockRemoved bool `json:"index_lock_removed,omitempty"`
}

// Failed reports whether the recorded sync actually failed at something git
// was asked to do. A blocked mirror is not a failure — refusing to clobber
// is the design.
func (s SyncState) Failed() bool {
	return s.FetchErr != "" || s.MergeErr != ""
}

// StateDir is the sidecar directory for a repos dir. An empty reposDir
// yields an empty path, which the writers treat as "nowhere to record" —
// that is how the tests and any caller without a mirror root opt out.
func StateDir(reposDir string) string {
	if reposDir == "" {
		return ""
	}
	return filepath.Join(reposDir, stateDirName)
}

// WriteState persists one mirror's last sync result, replacing whatever was
// there. The write is whole-file-then-rename so `rocket repo status`,
// reading concurrently from another process, never sees a half-written
// record and reports a mirror as broken over our own bookkeeping.
func WriteState(stateDir string, st SyncState) error {
	if stateDir == "" {
		return nil
	}
	if st.RepoID == "" {
		return fmt.Errorf("mirror: SyncState.RepoID must not be empty")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("mirror: create state dir %s: %w", stateDir, err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("mirror: marshal sync state for %s: %w", st.RepoID, err)
	}
	data = append(data, '\n')

	// The temp file gets a unique name, not <repo>.json.tmp. A fixed name is
	// shared by every writer of the same mirror, and writing truncates
	// first: one process would rename another's half-written file over the
	// final path, and ReadState would report the mirror as corrupt until the
	// next sync. That is the very race this feature exists to remove, and it
	// is reachable — the daemon's Syncer and a `rocket repo sync` are
	// separate processes. The mirror lock (a later task) would also close it,
	// but an atomic writer should be atomic on its own terms rather than on a
	// caller remembering to hold something.
	//
	// CreateTemp puts the file in stateDir, i.e. the same filesystem as the
	// final path, which is what keeps the rename atomic.
	final := statePath(stateDir, st.RepoID)
	f, err := os.CreateTemp(stateDir, st.RepoID+".*.tmp")
	if err != nil {
		return fmt.Errorf("mirror: create sync state temp for %s: %w", st.RepoID, err)
	}
	tmp := f.Name()

	if err := writeAndClose(f, data); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("mirror: write sync state for %s: %w", st.RepoID, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("mirror: publish sync state for %s: %w", st.RepoID, err)
	}
	return nil
}

// writeAndClose writes the record and closes the file, leaving it readable:
// CreateTemp makes the file 0o600, and the sidecar is meant to be readable
// by whoever runs `rocket repo status`, not only by whoever last synced.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadState returns the last recorded sync of one mirror. A mirror that has
// never been synced by a recording caller has no file, and that is the zero
// value and no error — "not known yet" is a state the CLI renders, not a
// failure. A file that exists but does not parse IS an error: silently
// reading corruption as "never synced" would hide exactly the mirror whose
// last sync went wrong.
func ReadState(stateDir, repoID string) (SyncState, error) {
	var st SyncState
	if stateDir == "" || repoID == "" {
		return st, nil
	}
	data, err := os.ReadFile(statePath(stateDir, repoID))
	if err != nil {
		if os.IsNotExist(err) {
			return SyncState{}, nil
		}
		return SyncState{}, fmt.Errorf("mirror: read sync state for %s: %w", repoID, err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return SyncState{}, fmt.Errorf("mirror: parse sync state for %s: %w", repoID, err)
	}
	return st, nil
}

// statePath is one mirror's sidecar file.
func statePath(stateDir, repoID string) string {
	return filepath.Join(stateDir, repoID+".json")
}

// SyncAndRecord runs Sync and persists the outcome for `rocket repo status`.
// A failure to persist is logged, never returned: the sync itself succeeded
// or failed on its own terms and must not be misreported over a sidecar.
//
// An empty stateDir records nothing and is not an error — plain Sync is
// still the entry point for tests and for callers with no repos dir.
func SyncAndRecord(ctx context.Context, repo store.Repo, stateDir, operation string) (SyncResult, error) {
	res, err := Sync(ctx, repo)
	RecordSync(repo.ID, stateDir, operation, res)
	return res, err
}

// RecordSync writes one sync's outcome to the sidecar. It is separate from
// SyncAndRecord because `repo sync --repair` finishes with Repair's own
// closing Sync, and that result — not the one from before the repair — is
// what the mirror's state actually is.
func RecordSync(repoID, stateDir, operation string, res SyncResult) {
	if stateDir == "" || repoID == "" {
		return
	}
	st := SyncState{
		RepoID:           repoID,
		At:               time.Now().UTC(),
		By:               operation,
		Blocked:          res.Blocked,
		IndexLockRemoved: res.IndexLockRemoved,
	}
	if res.FetchErr != nil {
		st.FetchErr = res.FetchErr.Error()
	}
	if res.MergeErr != nil {
		st.MergeErr = res.MergeErr.Error()
	}
	if err := WriteState(stateDir, st); err != nil {
		slog.Warn("mirror: cannot record sync state", "repo", repoID, "error", err)
	}
}

// The operation names recorded in SyncState.By — one per writer of a
// mirror. They are a closed set on purpose: `rocket repo status` says who
// last touched a mirror, and a free-form string there would be a vocabulary
// nobody could grep.
const (
	OpSyncer         = "syncer"
	OpRepoSync       = "repo-sync"
	OpRepoSyncRepair = "repo-sync-repair"
	OpWorkspaceClone = "workspace-clone"
)
