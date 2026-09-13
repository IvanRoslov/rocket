package mirror

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteThenReadState(t *testing.T) {
	dir := StateDir(t.TempDir())
	want := SyncState{RepoID: "rocket", At: time.Now().UTC().Truncate(time.Second), By: "syncer", MergeErr: "boom"}
	if err := WriteState(dir, want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	got, err := ReadState(dir, "rocket")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.MergeErr != want.MergeErr || got.By != want.By || !got.At.Equal(want.At) {
		t.Errorf("ReadState = %+v, want %+v", got, want)
	}
}

func TestReadStateMissingIsZero(t *testing.T) {
	got, err := ReadState(StateDir(t.TempDir()), "never-synced")
	if err != nil {
		t.Fatalf("ReadState on a missing file must not error: %v", err)
	}
	if got.RepoID != "" {
		t.Errorf("want zero SyncState, got %+v", got)
	}
}

func TestWriteStateOverwrites(t *testing.T) {
	dir := StateDir(t.TempDir())
	if err := WriteState(dir, SyncState{RepoID: "rocket", By: "syncer", MergeErr: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteState(dir, SyncState{RepoID: "rocket", By: "repo-sync"}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadState(dir, "rocket")
	if got.MergeErr != "" {
		t.Errorf("MergeErr = %q, want empty after a clean sync", got.MergeErr)
	}
}

// A half-written sidecar must be reported, not silently read as "never
// synced": rendering a corrupt file as a clean mirror is the same silence
// this whole feature exists to end.
func TestReadStateCorruptFileIsAnError(t *testing.T) {
	dir := StateDir(t.TempDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rocket.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(dir, "rocket"); err == nil {
		t.Fatal("ReadState on a corrupt file returned nil error")
	}
}

// WriteState must leave nothing but the final file behind: a .tmp left in
// the state dir would be read by nobody but would grow without bound.
func TestWriteStateLeavesNoTempFile(t *testing.T) {
	dir := StateDir(t.TempDir())
	if err := WriteState(dir, SyncState{RepoID: "rocket", By: "syncer"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "rocket.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir holds %v, want only [rocket.json]", names)
	}
}

// SyncAndRecord is the entry point every real caller uses; the sidecar it
// leaves is what `rocket repo status` reads.
func TestSyncAndRecordPersistsTheMergeError(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	lock := filepath.Join(repo.Path, ".git", "index.lock")
	writeFile(t, lock, "")
	defer func() { _ = os.Remove(lock) }()

	dir := StateDir(t.TempDir())
	if _, err := SyncAndRecord(t.Context(), repo, dir, "repo-sync"); err == nil {
		t.Fatal("SyncAndRecord: want the merge error, got nil")
	}

	st, err := ReadState(dir, repo.ID)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if st.MergeErr == "" {
		t.Error("sidecar MergeErr is empty; repo status would stay silent")
	}
	if st.By != "repo-sync" {
		t.Errorf("By = %q, want repo-sync", st.By)
	}
	if st.At.IsZero() {
		t.Error("At is zero; the sidecar must say when the sync happened")
	}
}

func TestSyncAndRecordWithoutStateDirStillSyncs(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	res, err := SyncAndRecord(t.Context(), repo, "", "syncer")
	if err != nil {
		t.Fatalf("SyncAndRecord: %v", err)
	}
	if res.Advanced != 1 {
		t.Errorf("Advanced = %d, want 1", res.Advanced)
	}
}
