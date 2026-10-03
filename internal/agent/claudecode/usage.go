package claudecode

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

type usageRecord struct {
	Type      string `json:"type"`
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			Output     int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type usageMessage struct {
	model  string
	tokens agent.Tokens
	at     time.Time
}

// Usage reads all Claude Code transcripts for a worktree, including child
// subagents belonging to each matching parent transcript.
func (c *ClaudeCode) Usage(ctx context.Context, worktreePath string, since time.Time) (agent.Usage, error) {
	dir := transcriptDir(worktreePath)
	if dir == "" {
		return agent.Usage{}, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return agent.Usage{}, nil
	}
	if err != nil {
		return agent.Usage{}, fmt.Errorf("read Claude transcript directory %s: %w", dir, err)
	}

	want := worktreePath
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		want = resolved
	}
	want = filepath.Clean(want)

	result := agent.Usage{Models: make(map[string]agent.Tokens)}
	messages := make(map[string]usageMessage)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		matched, err := scanClaudeUsageFile(ctx, path, want, since, false, messages, &result)
		if err != nil {
			return agent.Usage{}, err
		}
		if !matched {
			continue
		}
		result.Found = true
		subdir := filepath.Join(dir, strings.TrimSuffix(entry.Name(), ".jsonl"), "subagents")
		subs, err := os.ReadDir(subdir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return agent.Usage{}, fmt.Errorf("read Claude subagent directory %s: %w", subdir, err)
		}
		for _, sub := range subs {
			if sub.IsDir() || !strings.HasSuffix(sub.Name(), ".jsonl") {
				continue
			}
			if _, err := scanClaudeUsageFile(ctx, filepath.Join(subdir, sub.Name()), want, since, true, messages, &result); err != nil {
				return agent.Usage{}, err
			}
		}
	}
	for _, message := range messages {
		addClaudeTokens(result.Models, message.model, message.tokens)
	}
	return result, nil
}

func scanClaudeUsageFile(ctx context.Context, path, worktree string, since time.Time, subagent bool, messages map[string]usageMessage, result *agent.Usage) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open Claude transcript %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("stat Claude transcript %s: %w", path, err)
	}
	if info.ModTime().Before(since) {
		return false, nil
	}

	matched := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), usageLineLimit)
	resolvedCWDs := make(map[string]string)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var row usageRecord
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		if !subagent {
			cwd, known := resolvedCWDs[row.Cwd]
			if !known {
				cwd = row.Cwd
				if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
					cwd = resolved
				}
				cwd = filepath.Clean(cwd)
				resolvedCWDs[row.Cwd] = cwd
			}
			if cwd != worktree {
				continue
			}
		}
		at, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil {
			at = info.ModTime()
		}
		if at.Before(since) {
			continue
		}
		matched = true
		if result.FirstAt.IsZero() || at.Before(result.FirstAt) {
			result.FirstAt = at
		}
		if at.After(result.LastAt) {
			result.LastAt = at
		}
		if row.Type != "assistant" || row.Message.Usage == nil || row.Message.Model == "" || row.Message.Model == "<synthetic>" {
			continue
		}
		tokens := agent.Tokens{
			Input: row.Message.Usage.Input, CacheWrite: row.Message.Usage.CacheWrite,
			CacheRead: row.Message.Usage.CacheRead, Output: row.Message.Usage.Output,
			Messages: 1,
		}
		if row.Message.ID == "" {
			addClaudeTokens(result.Models, row.Message.Model, tokens)
			continue
		}
		if previous, ok := messages[row.Message.ID]; !ok || !at.Before(previous.at) {
			messages[row.Message.ID] = usageMessage{model: row.Message.Model, tokens: tokens, at: at}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read Claude transcript %s: %w", path, err)
	}
	return matched, nil
}

func addClaudeTokens(models map[string]agent.Tokens, model string, tokens agent.Tokens) {
	sum := models[model]
	sum.Input += tokens.Input
	sum.CacheWrite += tokens.CacheWrite
	sum.CacheRead += tokens.CacheRead
	sum.Output += tokens.Output
	sum.Messages += tokens.Messages
	models[model] = sum
}
