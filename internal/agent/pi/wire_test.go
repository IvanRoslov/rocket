package pi

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func writeSessionFile(t *testing.T, home, worktree, name string) string {
	t.Helper()
	physical, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimPrefix(physical, string(filepath.Separator))
	encoded = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(encoded)
	dir := filepath.Join(home, "sessions", "--"+encoded+"--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedSessionFiles(t *testing.T, home, worktree, otherWorktree string) (target, foreign string) {
	t.Helper()
	target = writeSessionFile(t, home, worktree, "2026-10-29_s1.jsonl")
	foreign = writeSessionFile(t, home, worktree, "2099-01-01_other.jsonl")
	writeSessionFile(t, home, otherWorktree, "2099-01-01_s1.jsonl")
	if err := os.Symlink(writeSessionFile(t, home, otherWorktree, "alias-source.jsonl"), filepath.Join(filepath.Dir(target), "0000_s1.jsonl")); err != nil {
		t.Fatal(err)
	}
	return target, foreign
}

func TestFindSession(t *testing.T) {
	home := t.TempDir()
	worktree := t.TempDir()
	otherWorktree := t.TempDir()
	target, _ := seedSessionFiles(t, home, worktree, otherWorktree)
	writeSessionFile(t, home, worktree, "0000_other_s1.jsonl")

	got, err := findSession(home, worktree, "s1")
	if err != nil || got != target {
		t.Fatalf("findSession = %q, %v; want exact regular file %q", got, err, target)
	}

	logicalWorktree := filepath.Join(t.TempDir(), "logical")
	if err := os.Symlink(worktree, logicalWorktree); err != nil {
		t.Fatal(err)
	}
	got, err = findSession(home, logicalWorktree, "s1")
	if err != nil || got != target {
		t.Fatalf("findSession through worktree symlink = %q, %v; want %q", got, err, target)
	}

	if _, err := findSession(home, worktree, "missing"); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("missing session error = %v, want ErrNoSignal", err)
	}
}

func TestFindWorktreeSessions(t *testing.T) {
	home := t.TempDir()
	worktree := t.TempDir()
	target, foreign := seedSessionFiles(t, home, worktree, t.TempDir())

	got, err := findWorktreeSessions(home, worktree)
	want := []string{target, foreign}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("findWorktreeSessions = %v, %v; want %v", got, err, want)
	}

	got, err = findWorktreeSessions(t.TempDir(), worktree)
	if err != nil || len(got) != 0 {
		t.Fatalf("absent session directory = %v, %v; want no paths and no error", got, err)
	}
}

func TestReadTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	big := strings.Repeat("x", 140_000)
	data := `{"type":"message","id":"big","parentId":null,"timestamp":"2026-10-29T17:14:03.557Z","payload":"` + big + `"}` + "\n" +
		`{"type":"message","id":"last","parentId":"big","timestamp":"2026-10-29T17:15:42.093Z"}` + "\n" +
		"{broken\n" + `{"type":"message","id":"partial","parentId":"last"`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	records, err := readTail(path, nil)
	if err != nil || len(records) != 2 {
		t.Fatalf("readTail returned %d records, %v; want 2", len(records), err)
	}
	if records[0].ID != "last" || records[0].ParentID != "big" || records[1].ID != "big" {
		t.Fatalf("readTail order/links = %+v", records)
	}
	wantTime := time.Date(2026, 10, 29, 17, 15, 42, 93_000_000, time.UTC)
	if !records[0].Timestamp.Equal(wantTime) {
		t.Fatalf("timestamp = %v, want %v", records[0].Timestamp, wantTime)
	}
	if !strings.Contains(string(records[1].Raw), big) {
		t.Fatal("large record Raw was truncated")
	}

	records, err = readTail(path, func(record sessionRecord) bool { return record.ID == "last" })
	if err != nil || len(records) != 1 || records[0].ID != "last" {
		t.Fatalf("stopped readTail = %+v, %v", records, err)
	}
}

func TestReadActiveBranch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		trailing string
	}{
		{name: "conversation leaf"},
		{name: "trailing usage", trailing: `{"type":"usage","id":"usage","parentId":"current","timestamp":"2026-10-29T17:15:42.093Z"}`},
		{name: "trailing custom", trailing: `{"type":"custom","id":"custom","parentId":"usage","timestamp":"2026-10-29T17:15:42.093Z"}`},
		{name: "trailing label", trailing: `{"type":"label","id":"label","parentId":"current","timestamp":"2026-10-29T17:15:42.093Z"}`},
		{name: "trailing session info", trailing: `{"type":"session_info","id":"info","parentId":"current","timestamp":"2026-10-29T17:15:42.093Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			lines := []string{
				`{"type":"session","id":"s1","timestamp":"2026-10-29T17:14:03.557Z"}`,
				`{"type":"model_change","id":"root","parentId":null,"timestamp":"2026-10-29T17:14:03.557Z"}`,
				`{"type":"message","id":"main","parentId":"root","timestamp":"2026-10-29T17:14:03.557Z"}`,
				`{"type":"message","id":"abandoned","parentId":"main","timestamp":"2026-10-29T17:14:03.557Z"}`,
				`{"type":"branch_summary","id":"current","parentId":"main","fromId":"abandoned","timestamp":"2026-10-29T17:14:03.557Z"}`,
			}
			if tc.name == "trailing custom" {
				lines = append(lines, `{"type":"usage","id":"usage","parentId":"current","timestamp":"2026-10-29T17:15:42.093Z"}`)
			}
			if tc.trailing != "" {
				lines = append(lines, tc.trailing)
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			records, err := readActiveBranch(path)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, record := range records {
				ids = append(ids, record.ID)
			}
			want := []string{"root", "main", "current"}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("active branch IDs = %v, want %v", ids, want)
			}
		})
	}
}
