// Package roles manages the on-disk home directory of an agent role:
// <home>/agents/<id>/role.md (the role prompt, editable without a restart —
// the next wake reads the current version) and <home>/agents/<id>/memory/
// (the role's file memory, MEMORY.md being the index).
//
// The memory interface is deliberately narrow (Ensure/ReadPrompt/MemoryIndex)
// so a different backend can be plugged in later without touching callers;
// v1 is files only. See the spec in task #639.
package roles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// memoryHeader seeds a fresh MEMORY.md. It is an index, one line per fact,
// with the facts themselves living in sibling files.
const memoryHeader = "<!-- Индекс памяти роли: одна строка на факт, ссылка на файл. -->\n"

// Dir returns the role home directory.
func Dir(home, id string) string {
	return filepath.Join(home, "agents", id)
}

// PromptPath returns the path of the managed role prompt copy.
func PromptPath(home, id string) string {
	return filepath.Join(Dir(home, id), "role.md")
}

// MemoryDir returns the role's memory directory.
func MemoryDir(home, id string) string {
	return filepath.Join(Dir(home, id), "memory")
}

// MemoryIndexPath returns the path of the role's memory index.
func MemoryIndexPath(home, id string) string {
	return filepath.Join(MemoryDir(home, id), "MEMORY.md")
}

// Ensure creates the role home directory tree and returns the path of the
// managed prompt copy. role.md is written when it does not exist yet, or when
// overwrite is true and prompt is non-empty; an existing MEMORY.md is never
// touched.
func Ensure(home, id, prompt string, overwrite bool) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("role id is empty")
	}

	if err := os.MkdirAll(MemoryDir(home, id), 0700); err != nil {
		return "", fmt.Errorf("create role memory dir: %w", err)
	}

	promptPath := PromptPath(home, id)
	_, err := os.Stat(promptPath)
	switch {
	case err == nil && (!overwrite || prompt == ""):
		// keep the existing prompt
	case err == nil || os.IsNotExist(err):
		if err := os.WriteFile(promptPath, []byte(prompt), 0600); err != nil {
			return "", fmt.Errorf("write role prompt: %w", err)
		}
	default:
		return "", fmt.Errorf("stat role prompt: %w", err)
	}

	indexPath := MemoryIndexPath(home, id)
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, []byte(memoryHeader), 0600); err != nil {
			return "", fmt.Errorf("write memory index: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat memory index: %w", err)
	}

	return promptPath, nil
}

// ReadPrompt returns the contents of a role prompt file. A missing file reads
// as an empty prompt so a role whose files were removed still lists and can be
// repaired, rather than breaking every read of it.
func ReadPrompt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read role prompt: %w", err)
	}
	return string(data), nil
}
