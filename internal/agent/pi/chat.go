package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/IvanRoslov/rocket/internal/agent"
)

const piChatDigestMaxRunes = 120

type piChatRecord struct {
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"content"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"message"`
}

func piChatDigest(raw json.RawMessage) string {
	var compact bytes.Buffer
	text := string(raw)
	if json.Compact(&compact, raw) == nil {
		text = compact.String()
	}
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= piChatDigestMaxRunes {
		return text
	}
	return string(runes[:piChatDigestMaxRunes-1]) + "…"
}

func piChatEntries(record sessionRecord) []agent.ChatEntry {
	var raw piChatRecord
	if json.Unmarshal(record.Raw, &raw) != nil {
		return nil
	}
	var ts int64
	if !record.Timestamp.IsZero() {
		ts = record.Timestamp.Unix()
	}
	var entries []agent.ChatEntry
	switch raw.Message.Role {
	case "user":
		var parts []string
		for _, part := range raw.Message.Content {
			if part.Type == "text" && part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) != 0 {
			entries = append(entries, agent.ChatEntry{Role: "user", Text: strings.Join(parts, "\n"), TS: ts})
		}
	case "assistant":
		for _, part := range raw.Message.Content {
			switch part.Type {
			case "text":
				if part.Text != "" {
					entries = append(entries, agent.ChatEntry{Role: "assistant", Text: part.Text, TS: ts})
				}
			case "toolCall":
				if part.Name != "" {
					entries = append(entries, agent.ChatEntry{Role: "tool", ToolName: part.Name, Text: piChatDigest(part.Arguments), TS: ts})
				}
			}
		}
		if raw.Message.StopReason == "error" && strings.TrimSpace(raw.Message.ErrorMessage) != "" {
			entries = append(entries, agent.ChatEntry{Role: "assistant", Text: "⚠ Pi turn failed: " + raw.Message.ErrorMessage, TS: ts})
		}
	}
	return entries
}

func (p *Pi) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	path, err := findSession(piHome(), ref.WorktreePath, ref.SessionID)
	if err != nil {
		return nil, "", err
	}
	records, err := readActiveBranch(path)
	if err != nil {
		return nil, "", fmt.Errorf("read Pi chat %s: %w", path, err)
	}

	start := 0
	lastID := ""
	if idx := strings.LastIndex(cursor, ":"); idx >= 0 && filepath.Clean(cursor[:idx]) == path {
		savedID := cursor[idx+1:]
		for i, record := range records {
			if savedID != "" && record.ID == savedID {
				start = i + 1
				lastID = savedID
				break
			}
		}
	}

	var entries []agent.ChatEntry
	for _, record := range records[start:] {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		entries = append(entries, piChatEntries(record)...)
		if record.ID != "" {
			lastID = record.ID
		}
	}
	return entries, path + ":" + lastID, nil
}

func (p *Pi) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	path, err := findSession(piHome(), ref.WorktreePath, ref.SessionID)
	if err != nil {
		return 0, 0, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, 0, agent.ErrNoSignal
	}
	return info.ModTime().Unix(), info.Size(), nil
}
