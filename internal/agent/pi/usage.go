package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

type piTokenUsage struct {
	Input      int64 `json:"input"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
}

type piUsageRecord struct {
	Type      string        `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	ModelID   string        `json:"modelId"`
	Usage     *piTokenUsage `json:"usage"`
	Message   struct {
		Role     string        `json:"role"`
		Provider string        `json:"provider"`
		Model    string        `json:"model"`
		Usage    *piTokenUsage `json:"usage"`
	} `json:"message"`
}

func piModelName(provider, model string) string {
	if provider == "" || model == "" {
		return ""
	}
	return provider + "/" + model
}

func (p *Pi) Usage(ctx context.Context, worktreePath string, since time.Time) (agent.Usage, error) {
	result := agent.Usage{Models: make(map[string]agent.Tokens)}
	if err := ctx.Err(); err != nil {
		return agent.Usage{}, err
	}
	paths, err := findWorktreeSessions(piHome(), worktreePath)
	if err != nil {
		return agent.Usage{}, err
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return agent.Usage{}, err
		}
		result.Found = true
		if err := scanPiUsage(ctx, path, since, &result); err != nil {
			return agent.Usage{}, err
		}
	}
	return result, nil
}

func scanPiUsage(ctx context.Context, path string, since time.Time, result *agent.Usage) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Pi journal %s: %w", path, err)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 64*1024)
	governingModel := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read Pi journal %s: %w", path, err)
		}
		var record piUsageRecord
		if json.Unmarshal(line, &record) != nil || record.Timestamp.IsZero() {
			continue
		}
		if record.Type == "model_change" {
			governingModel = piModelName(record.Provider, record.ModelID)
		}
		if governingModel == "" && record.Type == "message" && record.Message.Role == "assistant" {
			if model := piModelName(record.Message.Provider, record.Message.Model); model != "" {
				governingModel = model
			}
		}
		if record.Timestamp.Before(since) {
			continue
		}
		at := record.Timestamp
		if result.FirstAt.IsZero() || at.Before(result.FirstAt) {
			result.FirstAt = at
		}
		if at.After(result.LastAt) {
			result.LastAt = at
		}

		model := ""
		var usage *piTokenUsage
		switch record.Type {
		case "model_change":
			governingModel = piModelName(record.Provider, record.ModelID)
		case "message":
			if record.Message.Role == "assistant" {
				model = piModelName(record.Message.Provider, record.Message.Model)
				usage = record.Message.Usage
			}
		case "usage":
			model = piModelName(record.Provider, record.Model)
			usage = record.Usage
		case "compaction", "branch_summary":
			model = governingModel
			usage = record.Usage
		}
		if model == "" || usage == nil {
			continue
		}
		tokens := result.Models[model]
		tokens.Input += usage.Input
		tokens.CacheRead += usage.CacheRead
		tokens.CacheWrite += usage.CacheWrite
		tokens.Output += usage.Output
		tokens.Reasoning += usage.Reasoning
		tokens.Messages++
		result.Models[model] = tokens
	}
	return nil
}
