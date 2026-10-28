package kimicode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// findSession locates the newest native Kimi v2 session for a worktree. Kimi
// creates state.json only after the first prompt, so an absent match is a
// normal startup state.
func findSession(home, worktree string, since time.Time) (string, error) {
	physical, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", agent.ErrNoSignal
	}
	physical = filepath.Clean(physical)
	root := filepath.Join(home, "sessions")
	shards, err := os.ReadDir(root)
	if err != nil {
		return "", agent.ErrNoSignal
	}
	prefix := "wd_" + filepath.Base(physical) + "_"
	var preferred, all []string
	for _, shard := range shards {
		if !shard.IsDir() || !strings.HasPrefix(shard.Name(), "wd_") {
			continue
		}
		all = append(all, shard.Name())
		if strings.HasPrefix(shard.Name(), prefix) {
			preferred = append(preferred, shard.Name())
		}
	}
	if len(preferred) == 0 {
		preferred = all
	}
	var bestDir string
	var bestCreated int64
	for _, shard := range preferred {
		base := filepath.Join(root, shard)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "session_") {
				continue
			}
			dir := filepath.Join(base, entry.Name())
			b, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				continue
			}
			var state struct {
				Cwd       string `json:"cwd"`
				CreatedAt int64  `json:"createdAt"`
			}
			if json.Unmarshal(b, &state) != nil || filepath.Clean(state.Cwd) != physical || state.CreatedAt < since.UnixMilli() {
				continue
			}
			if bestDir == "" || state.CreatedAt > bestCreated || state.CreatedAt == bestCreated && dir > bestDir {
				bestDir, bestCreated = dir, state.CreatedAt
			}
		}
	}
	if bestDir == "" {
		return "", agent.ErrNoSignal
	}
	return bestDir, nil
}

func mainWire(sessionDir string) string {
	return filepath.Join(sessionDir, "agents", "main", "wire.jsonl")
}

func agentWires(sessionDir string) []string {
	root := filepath.Join(sessionDir, "agents")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "wire.jsonl")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

type wireRecord struct {
	Type string
	Time time.Time
	Raw  json.RawMessage
}

// readTail reads JSONL records newest first in fixed-size reverse blocks.
// A line may span arbitrarily many blocks; malformed and partial lines are
// ignored. stop is called after each parsed record and that record is kept.
func readTail(path string, stop func(wireRecord) bool) ([]wireRecord, error) {
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
	var records []wireRecord
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
			Type string `json:"type"`
			Time int64  `json:"time"`
		}
		if json.Unmarshal(line, &head) != nil || head.Type == "" {
			return false
		}
		rec := wireRecord{Type: head.Type, Time: time.UnixMilli(head.Time), Raw: line}
		records = append(records, rec)
		return stop != nil && stop(rec)
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
