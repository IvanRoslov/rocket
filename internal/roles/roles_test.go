package roles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesPromptAndMemoryIndex(t *testing.T) {
	home := t.TempDir()

	path, err := Ensure(home, "sre", "you are the platform SRE role", false)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if want := filepath.Join(home, "agents", "sre", "role.md"); path != want {
		t.Errorf("prompt path = %q, want %q", path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read role.md: %v", err)
	}
	if string(got) != "you are the platform SRE role" {
		t.Errorf("role.md = %q", got)
	}

	index, err := os.ReadFile(MemoryIndexPath(home, "sre"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if len(index) == 0 {
		t.Errorf("MEMORY.md is empty, want a header comment")
	}

	info, err := os.Stat(Dir(home, "sre"))
	if err != nil {
		t.Fatalf("stat role dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("role dir perm = %o, want 700", perm)
	}
}

func TestEnsureKeepsExistingPromptAndMemory(t *testing.T) {
	home := t.TempDir()

	if _, err := Ensure(home, "sre", "first", false); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := os.WriteFile(MemoryIndexPath(home, "sre"), []byte("- [fact](fact.md)\n"), 0600); err != nil {
		t.Fatalf("write memory index: %v", err)
	}

	if _, err := Ensure(home, "sre", "second", false); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	prompt, err := ReadPrompt(PromptPath(home, "sre"))
	if err != nil {
		t.Fatalf("ReadPrompt: %v", err)
	}
	if prompt != "first" {
		t.Errorf("prompt = %q, want first (no overwrite)", prompt)
	}

	index, err := os.ReadFile(MemoryIndexPath(home, "sre"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if string(index) != "- [fact](fact.md)\n" {
		t.Errorf("MEMORY.md = %q, want the user's content untouched", index)
	}
}

func TestEnsureOverwritesPrompt(t *testing.T) {
	home := t.TempDir()

	if _, err := Ensure(home, "sre", "first", false); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := Ensure(home, "sre", "second", true); err != nil {
		t.Fatalf("overwrite Ensure: %v", err)
	}

	prompt, err := ReadPrompt(PromptPath(home, "sre"))
	if err != nil {
		t.Fatalf("ReadPrompt: %v", err)
	}
	if prompt != "second" {
		t.Errorf("prompt = %q, want second", prompt)
	}
}

func TestEnsureEmptyID(t *testing.T) {
	if _, err := Ensure(t.TempDir(), "  ", "x", false); err == nil {
		t.Fatalf("Ensure with empty id: want error, got nil")
	}
}

func TestReadPromptMissingFile(t *testing.T) {
	prompt, err := ReadPrompt(filepath.Join(t.TempDir(), "nope.md"))
	if err != nil {
		t.Fatalf("ReadPrompt: %v", err)
	}
	if prompt != "" {
		t.Errorf("prompt = %q, want empty", prompt)
	}
}

func TestReadMemoryReturnsIndexAndFactFiles(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(MemoryDir(home, "sre"), "platform.md"), []byte("fact"), 0600); err != nil {
		t.Fatalf("write fact: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(MemoryDir(home, "sre"), "nested"), 0700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	index, files, err := ReadMemory(home, "sre")
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if index == "" {
		t.Fatal("index is empty, want the seeded MEMORY.md")
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v, want only the fact file (MEMORY.md and dirs excluded)", files)
	}
	if files[0].Name != "platform.md" || files[0].Body != "fact" || files[0].Size != 4 {
		t.Errorf("file = %+v", files[0])
	}
	if files[0].UpdatedAt == 0 {
		t.Error("updated_at = 0, want the file mtime")
	}
}

func TestReadMemoryOfUntouchedRole(t *testing.T) {
	home := t.TempDir()

	index, files, err := ReadMemory(home, "ghost")
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if index != "" || len(files) != 0 {
		t.Errorf("index = %q, files = %+v, want empty for a role with no memory dir", index, files)
	}
}

func TestWriteMemoryFileRoundTrips(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if err := WriteMemoryFile(home, "sre", "MEMORY.md", "- [Platform](platform.md)\n"); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := WriteMemoryFile(home, "sre", "platform.md", "how it deploys"); err != nil {
		t.Fatalf("write fact: %v", err)
	}

	index, files, err := ReadMemory(home, "sre")
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if index != "- [Platform](platform.md)\n" {
		t.Errorf("index = %q", index)
	}
	if len(files) != 1 || files[0].Body != "how it deploys" {
		t.Errorf("files = %+v", files)
	}
}

func TestWriteMemoryFileRejectsBadNames(t *testing.T) {
	home := t.TempDir()
	if _, err := Ensure(home, "sre", "role prompt", true); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, name := range []string{"", ".", "..", "../role.md", "a/b.md", "notes.txt", "/etc/passwd.md", "..md"} {
		if err := WriteMemoryFile(home, "sre", name, "x"); err == nil {
			t.Errorf("WriteMemoryFile(%q) = nil, want an error", name)
		} else if !errors.Is(err, ErrInvalidMemoryFile) {
			t.Errorf("WriteMemoryFile(%q) error = %v, want ErrInvalidMemoryFile", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "agents", "sre", "role.md")); err != nil {
		t.Fatalf("role.md damaged by a rejected write: %v", err)
	}
}
