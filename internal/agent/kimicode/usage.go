package kimicode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

type kimiUsageRecord struct {
	Type  string `json:"type"`
	Time  int64  `json:"time"`
	Model string `json:"model"`
	Usage *struct {
		InputOther         int64 `json:"inputOther"`
		InputCacheRead     int64 `json:"inputCacheRead"`
		InputCacheCreation int64 `json:"inputCacheCreation"`
		Output             int64 `json:"output"`
	} `json:"usage"`
}

// Kimi records the physical cwd. If a finished worker's worktree is gone,
// resolve its nearest surviving parent so old journals remain attributable.
func kimiUsageWorktreePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	var suffix []string
	for current := abs; ; {
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs)
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Clean(filepath.Join(append([]string{resolved}, suffix...)...))
		}
	}
}

func kimiUsageAgentWires(sessionDir string) ([]string, error) {
	root := filepath.Join(sessionDir, "agents")
	agents, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Kimi agents %s: %w", root, err)
	}
	var paths []string
	for _, entry := range agents {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "wire.jsonl")
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat Kimi journal %s: %w", path, err)
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// Usage sums usage.record rows from every matching native session and agent
// journal. A subagent.completed row only repeats its child journal's usage.
func (k *KimiCode) Usage(ctx context.Context, worktreePath string, since time.Time) (agent.Usage, error) {
	result := agent.Usage{Models: make(map[string]agent.Tokens)}
	if err := ctx.Err(); err != nil {
		return agent.Usage{}, err
	}
	physical := kimiUsageWorktreePath(worktreePath)
	root := filepath.Join(kimiHome(), "sessions")
	shards, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return agent.Usage{}, fmt.Errorf("read Kimi sessions %s: %w", root, err)
	}
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return agent.Usage{}, err
		}
		if !shard.IsDir() || !strings.HasPrefix(shard.Name(), "wd_") {
			continue
		}
		shardDir := filepath.Join(root, shard.Name())
		sessions, err := os.ReadDir(shardDir)
		if err != nil {
			return agent.Usage{}, fmt.Errorf("read Kimi session shard %s: %w", shardDir, err)
		}
		for _, session := range sessions {
			if err := ctx.Err(); err != nil {
				return agent.Usage{}, err
			}
			if !session.IsDir() || !strings.HasPrefix(session.Name(), "session_") {
				continue
			}
			dir := filepath.Join(shardDir, session.Name())
			statePath := filepath.Join(dir, "state.json")
			stateBytes, err := os.ReadFile(statePath)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return agent.Usage{}, fmt.Errorf("read Kimi state %s: %w", statePath, err)
			}
			var state struct {
				Cwd       string `json:"cwd"`
				CreatedAt int64  `json:"createdAt"`
			}
			if json.Unmarshal(stateBytes, &state) != nil || filepath.Clean(state.Cwd) != physical {
				continue
			}
			paths, err := kimiUsageAgentWires(dir)
			if err != nil {
				return agent.Usage{}, err
			}
			for _, path := range paths {
				if state.CreatedAt >= since.UnixMilli() {
					result.Found = true // An empty current journal still exists.
				}
				if err := scanKimiUsage(ctx, path, since, &result); err != nil {
					return agent.Usage{}, err
				}
			}
		}
	}
	return result, nil
}

func scanKimiUsage(ctx context.Context, path string, since time.Time, result *agent.Usage) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Kimi journal %s: %w", path, err)
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break // A trailing partial record can be completed by the writer.
		}
		if err != nil {
			return fmt.Errorf("read Kimi journal %s: %w", path, err)
		}
		var row kimiUsageRecord
		if json.Unmarshal(line, &row) != nil || row.Time == 0 {
			continue
		}
		at := time.UnixMilli(row.Time)
		if at.Before(since) {
			continue
		}
		result.Found = true
		if result.FirstAt.IsZero() || at.Before(result.FirstAt) {
			result.FirstAt = at
		}
		if at.After(result.LastAt) {
			result.LastAt = at
		}
		if row.Type != "usage.record" || row.Model == "" || row.Usage == nil {
			continue
		}
		tokens := result.Models[row.Model]
		tokens.Input += row.Usage.InputOther
		tokens.CacheRead += row.Usage.InputCacheRead
		tokens.CacheWrite += row.Usage.InputCacheCreation
		tokens.Output += row.Usage.Output
		tokens.Messages++
		result.Models[row.Model] = tokens
	}
	return nil
}
