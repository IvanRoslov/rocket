package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func worktreeSessionDir(home, worktree string) (string, error) {
	physical, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", err
	}
	physical = filepath.Clean(physical)
	encoded := strings.TrimPrefix(physical, string(filepath.Separator))
	encoded = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(encoded)
	return filepath.Join(home, "sessions", "--"+encoded+"--"), nil
}

func findWorktreeSessions(home, worktree string) ([]string, error) {
	dir, err := worktreeSessionDir(home, worktree)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Pi session directory %s: %w", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat Pi session %s: %w", entry.Name(), err)
		}
		if info.Mode().IsRegular() {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func findSession(home, worktree, sessionID string) (string, error) {
	paths, err := findWorktreeSessions(home, worktree)
	if os.IsNotExist(err) {
		return "", agent.ErrNoSignal
	}
	if err != nil {
		return "", err
	}
	suffix := "_" + sessionID + ".jsonl"
	for _, path := range paths {
		if strings.HasSuffix(filepath.Base(path), suffix) {
			return path, nil
		}
	}
	return "", agent.ErrNoSignal
}

type sessionRecord struct {
	Type      string
	ID        string
	ParentID  string
	Timestamp time.Time
	Raw       json.RawMessage
}

func readTail(path string, stop func(sessionRecord) bool) ([]sessionRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	const blockSize = 64 * 1024
	block := make([]byte, blockSize)
	lineRev := make([]byte, 0, blockSize)
	var records []sessionRecord
	parseLine := func() bool {
		if len(lineRev) == 0 {
			return false
		}
		line := make([]byte, len(lineRev))
		for i := range lineRev {
			line[len(lineRev)-1-i] = lineRev[i]
		}
		lineRev = lineRev[:0]
		var head struct {
			Type      string    `json:"type"`
			ID        string    `json:"id"`
			ParentID  string    `json:"parentId"`
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &head) != nil || head.Type == "" {
			return false
		}
		record := sessionRecord{Type: head.Type, ID: head.ID, ParentID: head.ParentID, Timestamp: head.Timestamp, Raw: line}
		records = append(records, record)
		return stop != nil && stop(record)
	}

	for end := info.Size(); end > 0; {
		start := end - blockSize
		if start < 0 {
			start = 0
		}
		n, err := f.ReadAt(block[:end-start], start)
		if err != nil {
			return nil, err
		}
		for i := n - 1; i >= 0; i-- {
			if block[i] == '\n' {
				if parseLine() {
					return records, nil
				}
			} else {
				lineRev = append(lineRev, block[i])
			}
		}
		end = start
	}
	parseLine()
	return records, nil
}

func readActiveBranch(path string) ([]sessionRecord, error) {
	records, err := readTail(path, nil)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]sessionRecord, len(records))
	leaf := -1
	for i, record := range records {
		if record.ID != "" {
			byID[record.ID] = record
		}
		if leaf == -1 && record.ID != "" && record.Type != "session" && record.Type != "usage" && record.Type != "custom" {
			leaf = i
		}
	}
	if leaf == -1 {
		return nil, nil
	}

	var branch []sessionRecord
	visited := map[string]bool{}
	current := records[leaf]
	for {
		if current.ID != "" {
			if visited[current.ID] {
				break
			}
			visited[current.ID] = true
		}
		branch = append(branch, current)
		if current.ParentID == "" {
			break
		}
		parent, ok := byID[current.ParentID]
		if !ok {
			break
		}
		current = parent
	}
	for left, right := 0, len(branch)-1; left < right; left, right = left+1, right-1 {
		branch[left], branch[right] = branch[right], branch[left]
	}
	return branch, nil
}
