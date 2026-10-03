package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// Scanner needs room for a full 16 MiB JSONL record plus the line delimiter.
const usageLineLimit = 16*1024*1024 + 64*1024

type cumulativeUsage struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}

type codexUsageRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Cwd   string `json:"cwd"`
		Model string `json:"model"`
		Type  string `json:"type"`
		Info  *struct {
			Total *cumulativeUsage `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// Usage reads every rollout for the worktree in date shards from since to
// today. Each token_count total is cumulative within its rollout file.
func (c *Codex) Usage(ctx context.Context, worktreePath string, since time.Time) (agent.Usage, error) {
	root := sessionsRoot()
	if root == "" {
		return agent.Usage{}, nil
	}
	want := resolvedUsagePath(worktreePath)
	result := agent.Usage{Models: make(map[string]agent.Tokens)}
	day := since.In(time.Local)
	if since.IsZero() {
		day = time.Unix(0, 0).In(time.Local)
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	today := time.Now().In(time.Local)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	for !day.After(today) {
		if err := ctx.Err(); err != nil {
			return agent.Usage{}, err
		}
		dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			return agent.Usage{}, fmt.Errorf("read Codex session directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "rollout-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			if err := scanCodexUsageFile(ctx, filepath.Join(dir, entry.Name()), want, since, &result); err != nil {
				return agent.Usage{}, err
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return result, nil
}

func resolvedUsagePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func scanCodexUsageFile(ctx context.Context, path, worktree string, since time.Time, result *agent.Usage) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Codex rollout %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat Codex rollout %s: %w", path, err)
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), usageLineLimit)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read Codex rollout %s: %w", path, err)
		}
		return nil
	}
	var meta codexUsageRecord
	if json.Unmarshal(scanner.Bytes(), &meta) != nil || meta.Type != "session_meta" || resolvedUsagePath(meta.Payload.Cwd) != worktree {
		return nil
	}
	observeCodexTimestamp(meta.Timestamp, info.ModTime(), since, result)

	var previous cumulativeUsage
	model := ""
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var row codexUsageRecord
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		_, included := observeCodexTimestamp(row.Timestamp, info.ModTime(), since, result)
		if row.Type == "turn_context" && row.Payload.Model != "" {
			model = row.Payload.Model
			continue
		}
		if row.Type != "event_msg" || row.Payload.Type != "token_count" || row.Payload.Info == nil || row.Payload.Info.Total == nil {
			continue
		}
		current := *row.Payload.Info.Total
		if included && model != "" && current.Total != previous.Total {
			base := previous
			if current.Total < previous.Total {
				base = cumulativeUsage{}
			}
			messages := int64(0)
			if current.Total > previous.Total {
				messages = 1
			}
			delta := agent.Tokens{
				Input:      nonnegative(current.Input-base.Input) - nonnegative(current.Cached-base.Cached),
				CacheRead:  nonnegative(current.Cached - base.Cached),
				CacheWrite: nonnegative(current.CacheWrite - base.CacheWrite),
				Output:     nonnegative(current.Output - base.Output),
				Reasoning:  nonnegative(current.Reasoning - base.Reasoning),
				Messages:   messages,
			}
			delta.Input = nonnegative(delta.Input)
			sum := result.Models[model]
			sum.Input += delta.Input
			sum.CacheWrite += delta.CacheWrite
			sum.CacheRead += delta.CacheRead
			sum.Output += delta.Output
			sum.Reasoning += delta.Reasoning
			sum.Messages += delta.Messages
			result.Models[model] = sum
		}
		previous = current
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Codex rollout %s: %w", path, err)
	}
	return nil
}

func nonnegative(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func observeCodexTimestamp(raw string, fallback time.Time, since time.Time, result *agent.Usage) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		at = fallback
	}
	if at.Before(since) {
		return at, false
	}
	result.Found = true
	if result.FirstAt.IsZero() || at.Before(result.FirstAt) {
		result.FirstAt = at
	}
	if at.After(result.LastAt) {
		result.LastAt = at
	}
	return at, true
}
