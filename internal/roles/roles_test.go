package roles

import (
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
