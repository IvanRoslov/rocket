package nativeinput

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

type SenderFunc func(ctx context.Context, target Target, messageID int64, text string) (Outcome, error)

func (f SenderFunc) Send(ctx context.Context, target Target, messageID int64, text string) (Outcome, error) {
	return f(ctx, target, messageID, text)
}

func Send(ctx context.Context, target Target, messageID int64, text string) (Outcome, error) {
	if target.SessionID == "" || target.InstanceID == "" || target.SocketPath == "" || messageID <= 0 {
		return NotSent, fmt.Errorf("invalid native delivery target")
	}
	frame, err := json.Marshal(Request{
		Version: Version, SessionID: target.SessionID, InstanceID: target.InstanceID,
		MessageID: messageID, Text: text,
	})
	if err != nil {
		return NotSent, fmt.Errorf("encode native request: %w", err)
	}
	frame = append(frame, '\n')
	if len(frame) > MaxFrameSize {
		return NotSent, fmt.Errorf("native request exceeds 1 MiB frame limit")
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", target.SocketPath)
	if err != nil {
		return NotSent, fmt.Errorf("dial native endpoint: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stopCancel()

	n, err := conn.Write(frame)
	if err != nil {
		return Unknown, fmt.Errorf("write native request: %w", err)
	}
	if n != len(frame) {
		return Unknown, fmt.Errorf("write native request: %w", io.ErrShortWrite)
	}

	line, err := bufio.NewReader(io.LimitReader(conn, MaxFrameSize+1)).ReadBytes('\n')
	if len(line) > MaxFrameSize {
		return Unknown, fmt.Errorf("native response exceeds 1 MiB frame limit")
	}
	if err != nil {
		return Unknown, fmt.Errorf("read native response: %w", err)
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return Unknown, fmt.Errorf("native response is not newline-terminated")
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Unknown, fmt.Errorf("decode native response: %w", err)
	}
	if resp.Version != Version || resp.SessionID != target.SessionID || resp.InstanceID != target.InstanceID || resp.MessageID != messageID {
		return Unknown, fmt.Errorf("native response identity mismatch")
	}
	switch Outcome(resp.Outcome) {
	case Accepted:
		return Accepted, nil
	case NotSent:
		return NotSent, nil
	default:
		return Unknown, fmt.Errorf("invalid native response outcome %q", resp.Outcome)
	}
}
