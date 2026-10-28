package kimicode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

const kimiChatDigestMaxRunes = 120

type kimiChatRecord struct {
	Type    string `json:"type"`
	Time    int64  `json:"time"`
	Message struct {
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	} `json:"message"`
	Event struct {
		Type    string          `json:"type"`
		Name    string          `json:"name"`
		Args    json.RawMessage `json:"args"`
		Display struct {
			Command string `json:"command"`
		} `json:"display"`
	} `json:"event"`
}

func kimiChatDigest(raw string) string {
	text := strings.Join(strings.Fields(raw), " ")
	runes := []rune(text)
	if len(runes) <= kimiChatDigestMaxRunes {
		return text
	}
	return string(runes[:kimiChatDigestMaxRunes-1]) + "…"
}

func kimiChatEntry(line []byte) (agent.ChatEntry, bool) {
	var rec kimiChatRecord
	if json.Unmarshal(line, &rec) != nil {
		return agent.ChatEntry{}, false
	}
	var ts int64
	if rec.Time != 0 {
		ts = time.UnixMilli(rec.Time).Unix()
	}
	switch rec.Type {
	case "agent.message.appended":
		role := rec.Message.Message.Role
		if role != "user" && role != "assistant" {
			return agent.ChatEntry{}, false
		}
		var parts []string
		for _, part := range rec.Message.Message.Content {
			if part.Type == "text" && part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) == 0 {
			return agent.ChatEntry{}, false
		}
		return agent.ChatEntry{Role: role, Text: strings.Join(parts, "\n"), TS: ts}, true
	case "context.append_loop_event":
		if rec.Event.Type != "tool.call" || rec.Event.Name == "" {
			return agent.ChatEntry{}, false
		}
		input := rec.Event.Display.Command
		if input == "" && len(rec.Event.Args) > 0 {
			var compact bytes.Buffer
			if json.Compact(&compact, rec.Event.Args) == nil {
				input = compact.String()
			}
		}
		return agent.ChatEntry{Role: "tool", ToolName: rec.Event.Name, Text: kimiChatDigest(input), TS: ts}, true
	default:
		return agent.ChatEntry{}, false
	}
}

func readKimiChatFrom(ctx context.Context, path string, offset int64) ([]agent.ChatEntry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	var entries []agent.ChatEntry
	pos := offset
	for {
		if err := ctx.Err(); err != nil {
			return nil, offset, err
		}
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break // The writer may still be appending this record.
		}
		if err != nil {
			return nil, offset, err
		}
		pos += int64(len(line))
		if entry, ok := kimiChatEntry(line); ok {
			entries = append(entries, entry)
		}
	}
	return entries, pos, nil
}

// TranscriptTail reads complete records from the current main-agent journal.
// The cursor is the journal path and last complete byte offset. A cursor for
// an older session or truncated journal restarts at offset zero.
func (k *KimiCode) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	session, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return nil, "", err
	}
	path := mainWire(session)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, "", agent.ErrNoSignal
	}
	var offset int64
	if idx := strings.LastIndex(cursor, ":"); idx >= 0 && filepath.Clean(cursor[:idx]) == path {
		if parsed, err := strconv.ParseInt(cursor[idx+1:], 10, 64); err == nil && parsed >= 0 && parsed <= info.Size() {
			offset = parsed
		}
	}
	entries, next, err := readKimiChatFrom(ctx, path, offset)
	if err != nil {
		return nil, "", fmt.Errorf("read Kimi chat %s: %w", path, err)
	}
	return entries, fmt.Sprintf("%s:%d", path, next), nil
}

// TranscriptStat reports the current main-agent journal's mtime and size.
func (k *KimiCode) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	session, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return 0, 0, err
	}
	info, err := os.Stat(mainWire(session))
	if err != nil || !info.Mode().IsRegular() {
		return 0, 0, agent.ErrNoSignal
	}
	return info.ModTime().Unix(), info.Size(), nil
}
