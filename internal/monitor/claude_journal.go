package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"io"
	"os"
	"time"
)

// claudeJournalActivity reads lifecycle metadata only from the exact verified
// UUID journal. The adapter's newest-transcript UI selection is not authority
// for another root's readiness or API-error block.
func claudeJournalActivity(ctx context.Context, id claudeinstance.Identity) (activity.State, time.Time, error) {
	info, e := os.Lstat(id.JournalPath)
	if e != nil || !info.Mode().IsRegular() {
		return "", time.Time{}, agent.ErrNoSignal
	}
	f, e := os.Open(id.JournalPath)
	if e != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(info, actual) || actual.ModTime().Before(id.ProcessStart) {
		return "", time.Time{}, agent.ErrNoSignal
	}
	if actual.Size() == 0 {
		return "", time.Time{}, agent.ErrNoSignal
	}
	var tail [1]byte
	if _, e = f.ReadAt(tail[:], actual.Size()-1); e != nil || tail[0] != '\n' {
		return "", time.Time{}, agent.ErrNoSignal
	}
	start := actual.Size() - (1 << 20)
	if start < 0 {
		start = 0
	}
	if _, e = f.Seek(start, io.SeekStart); e != nil {
		return "", time.Time{}, e
	}
	scanner := bufio.NewScanner(io.LimitReader(f, actual.Size()-start))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	first := true
	found, blocked := false, false
	var lastAt time.Time
	for scanner.Scan() {
		if e = ctx.Err(); e != nil {
			return "", time.Time{}, e
		}
		if first && start > 0 {
			first = false
			continue
		}
		first = false
		var record struct {
			SessionID string    `json:"sessionId"`
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Error     bool      `json:"isApiErrorMessage"`
			Message   struct {
				Error bool `json:"isApiErrorMessage"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return "", time.Time{}, agent.ErrNoSignal
		}
		if record.SessionID != id.ClaudeSessionID {
			continue
		}
		if record.Timestamp.IsZero() || record.Timestamp.Before(id.ProcessStart) || record.Timestamp.After(time.Now()) {
			return "", time.Time{}, agent.ErrNoSignal
		}
		lastAt = record.Timestamp
		found = true
		blocked = record.Error || record.Message.Error
	}
	if e = scanner.Err(); e != nil {
		return "", time.Time{}, e
	}
	if !found {
		return "", time.Time{}, agent.ErrNoSignal
	}
	if blocked {
		return activity.Blocked, lastAt, nil
	}
	if time.Since(lastAt) < 30*time.Second {
		return activity.Active, lastAt, nil
	}
	return activity.Ready, lastAt, nil
}
