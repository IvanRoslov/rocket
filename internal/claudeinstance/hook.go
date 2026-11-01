package claudeinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

const MaxHookBytes = 16 << 10

type HookEvent struct {
	ClaudeSessionID  string    `json:"session_id"`
	HookEventName    string    `json:"hook_event_name"`
	Reason           string    `json:"reason,omitempty"`
	AgentID          string    `json:"agent_id,omitempty"`
	LaunchGeneration string    `json:"launch_generation"`
	ProducerPID      int       `json:"producer_pid"`
	ProducerStart    time.Time `json:"producer_start"`
	At               int64     `json:"at"`
}

func ParseHook(r io.Reader) (HookEvent, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxHookBytes+1))
	if err != nil {
		return HookEvent{}, err
	}
	if len(raw) > MaxHookBytes {
		return HookEvent{}, fmt.Errorf("hook payload too large")
	}
	var event HookEvent
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err = dec.Decode(&event); err != nil {
		return HookEvent{}, fmt.Errorf("invalid hook metadata")
	}
	if dec.Decode(new(any)) != io.EOF {
		return HookEvent{}, fmt.Errorf("trailing hook payload")
	}
	return event, nil
}
func ValidateHook(ctx context.Context, id Identity, event HookEvent, state string, last int64, now time.Time, probe Probe) error {
	invalid := func() error { return fmt.Errorf("unqualified Claude activity") }
	if event.AgentID != "" || event.ClaudeSessionID != id.ClaudeSessionID || event.LaunchGeneration != id.LaunchGeneration || event.At <= last || event.At <= 0 || event.At > now.UnixNano() || event.At < id.ProcessStart.UnixNano() || event.ProducerPID <= 0 || event.ProducerStart.IsZero() || probe == nil {
		return invalid()
	}
	expected := map[string]string{"UserPromptSubmit": "active", "PreToolUse": "active", "PostToolUse": "active", "Stop": "ready", "Notification": "waiting_input", "SessionEnd": "exited"}
	if expected[event.HookEventName] != state {
		return invalid()
	}
	process, exists, err := probe.Process(ctx, event.ProducerPID)
	if err != nil || !exists || !process.Start.Equal(event.ProducerStart) {
		return invalid()
	}
	seen := map[int]bool{}
	for {
		if seen[process.PID] || len(seen) > 64 {
			return invalid()
		}
		seen[process.PID] = true
		if process.PID == id.PID {
			if !process.Start.Equal(id.ProcessStart) || filepath.Base(process.Name) != "claude" {
				return invalid()
			}
			return nil
		}
		if filepath.Base(process.Name) == "claude" || process.PPID <= 1 {
			return invalid()
		}
		process, exists, err = probe.Process(ctx, process.PPID)
		if err != nil || !exists {
			return invalid()
		}
	}
}
