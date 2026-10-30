package pi

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

const freshWindow = 30 * time.Second

type conversationMessage struct {
	Role         string `json:"role"`
	StopReason   string `json:"stopReason"`
	ErrorMessage string `json:"errorMessage"`
	Content      []struct {
		Type string `json:"type"`
	} `json:"content"`
}

func latestConversation(records []sessionRecord) (sessionRecord, conversationMessage, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Type != "message" {
			continue
		}
		var envelope struct {
			Message conversationMessage `json:"message"`
		}
		if json.Unmarshal(records[i].Raw, &envelope) != nil || envelope.Message.Role == "system" {
			continue
		}
		return records[i], envelope.Message, true
	}
	return sessionRecord{}, conversationMessage{}, false
}

func conversationState(records []sessionRecord, mtime time.Time) activity.State {
	_, msg, ok := latestConversation(records)
	if ok {
		switch {
		case msg.Role == "assistant" && msg.StopReason == "error":
			return activity.Blocked
		case msg.Role == "user" || msg.Role == "toolResult":
			return activity.Active
		case msg.Role == "assistant" && msg.StopReason == "toolUse":
			return activity.Active
		case msg.Role == "assistant":
			for _, part := range msg.Content {
				if part.Type == "toolCall" {
					return activity.Active
				}
			}
		}
	}
	if time.Since(mtime) < freshWindow {
		return activity.Active
	}
	return activity.Ready
}

var _ agent.TurnFailureReporter = (*Pi)(nil)

func (p *Pi) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}
	path, err := findSession(piHome(), ref.WorktreePath, ref.SessionID)
	if err != nil {
		return "", time.Time{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, err
	}
	records, err := readActiveBranch(path)
	if err != nil {
		return "", time.Time{}, err
	}
	if _, _, ok := latestConversation(records); !ok {
		return "", time.Time{}, agent.ErrNoSignal
	}
	return conversationState(records, info.ModTime()), info.ModTime(), nil
}

func (p *Pi) LastTurnFailure(ctx context.Context, ref agent.ActivityRef) (string, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	path, err := findSession(piHome(), ref.WorktreePath, ref.SessionID)
	if err != nil {
		return "", "", false, err
	}
	records, err := readActiveBranch(path)
	if err != nil {
		return "", "", false, err
	}
	rec, msg, ok := latestConversation(records)
	if !ok || msg.Role != "assistant" || msg.StopReason != "error" {
		return "", "", false, nil
	}
	return ref.SessionID + "/" + rec.ID, msg.ErrorMessage, true, nil
}
