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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// MemoryFile is one fact file of a role's file memory. Bodies are inlined:
// role memory is a handful of short markdown notes, and both the dashboard and
// the mobile app render all of them at once.
type MemoryFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updated_at"`
	Body      string `json:"body"`
}

// ErrInvalidMemoryFile is returned for a memory file name that is not a plain
// markdown base name.
var ErrInvalidMemoryFile = errors.New("invalid memory file name")

// memoryFileName matches an acceptable memory file name: a base name with no
// path separators, markdown, at least one character before the suffix.
var memoryFileName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.md$`)

// ValidMemoryFileName reports whether name may be written into a role's memory
// directory. It is deliberately strict: the name arrives in an HTTP body and
// the memory directory sits right next to the role prompt.
func ValidMemoryFileName(name string) bool {
	if strings.Contains(name, "..") {
		return false
	}
	if !memoryFileName.MatchString(name) {
		return false
	}
	return filepath.Base(name) == name
}

// ReadMemory returns the role's memory index (MEMORY.md) and the fact files
// beside it, sorted by name. A role whose memory directory does not exist yet
// reads as empty rather than an error — the same forgiving rule ReadPrompt
// follows, so a half-created role still lists and can be repaired.
func ReadMemory(home, id string) (string, []MemoryFile, error) {
	index, err := os.ReadFile(MemoryIndexPath(home, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("read memory index: %w", err)
	}

	entries, err := os.ReadDir(MemoryDir(home, id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return string(index), []MemoryFile{}, nil
		}
		return "", nil, fmt.Errorf("read memory dir: %w", err)
	}

	files := make([]MemoryFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", nil, fmt.Errorf("stat memory file %s: %w", e.Name(), err)
		}
		body, err := os.ReadFile(filepath.Join(MemoryDir(home, id), e.Name()))
		if err != nil {
			return "", nil, fmt.Errorf("read memory file %s: %w", e.Name(), err)
		}
		files = append(files, MemoryFile{
			Name:      e.Name(),
			Size:      info.Size(),
			UpdatedAt: info.ModTime().Unix(),
			Body:      string(body),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	return string(index), files, nil
}

// WriteMemoryFile creates or replaces one file of the role's memory (the
// MEMORY.md index included). Beyond the name check it re-resolves the joined
// path and refuses anything that would land outside the memory directory.
func WriteMemoryFile(home, id, name, body string) error {
	if !ValidMemoryFileName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidMemoryFile, name)
	}

	dir := MemoryDir(home, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create role memory dir: %w", err)
	}

	path := filepath.Join(dir, name)
	if filepath.Dir(path) != filepath.Clean(dir) {
		return fmt.Errorf("%w: %q escapes the memory directory", ErrInvalidMemoryFile, name)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}
	return nil
}
