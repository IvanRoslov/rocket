package claudeinstance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

type JournalBoundary struct {
	Path          string
	Device, Inode uint64
	Offset        int64
}
type Snapshot struct {
	EnvelopeVersion                    uint8
	MessageID                          int64
	Transport                          string
	Target                             Identity
	WireID, Envelope, ReplyPath, Phase string
	Boundary                           JournalBoundary
	PreparedAt, Deadline               time.Time
}
type Proof struct {
	RecordID    string
	Offset, End int64
}

func openJournal(path string) (*os.File, os.FileInfo, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("journal is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		f.Close()
		return nil, nil, fmt.Errorf("journal changed during open")
	}
	return f, actual, nil
}
func fileID(st os.FileInfo) (uint64, uint64, error) {
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("journal file identity unavailable")
	}
	return uint64(v.Dev), uint64(v.Ino), nil
}
func PinJournal(id Identity) (JournalBoundary, error) {
	f, st, err := openJournal(id.JournalPath)
	if err != nil {
		return JournalBoundary{}, err
	}
	defer f.Close()
	dev, ino, err := fileID(st)
	if err != nil {
		return JournalBoundary{}, err
	}
	if st.Size() > 0 {
		var b [1]byte
		if _, err := f.ReadAt(b[:], st.Size()-1); err != nil {
			return JournalBoundary{}, err
		}
		if b[0] != '\n' {
			return JournalBoundary{}, fmt.Errorf("journal has an incomplete tail")
		}
	}
	return JournalBoundary{Path: id.JournalPath, Device: dev, Inode: ino, Offset: st.Size()}, nil
}
func FindProof(ctx context.Context, s Snapshot) (Proof, bool, error) {
	if s.Boundary.Path != s.Target.JournalPath || s.Boundary.Offset < 0 {
		return Proof{}, false, fmt.Errorf("journal boundary mismatch")
	}
	f, st, err := openJournal(s.Boundary.Path)
	if err != nil {
		return Proof{}, false, err
	}
	defer f.Close()
	dev, ino, err := fileID(st)
	if err != nil {
		return Proof{}, false, err
	}
	if dev != s.Boundary.Device || ino != s.Boundary.Inode || st.Size() < s.Boundary.Offset {
		return Proof{}, false, fmt.Errorf("journal replaced or truncated")
	}
	if _, err = f.Seek(s.Boundary.Offset, io.SeekStart); err != nil {
		return Proof{}, false, err
	}
	// Bound the whole fixed-size tail, including metadata and JSON escaping.
	// A complete record may be larger than the socket frame; a per-record
	// cutoff would hide both legal exact envelopes and later duplicates.
	// An oversized total scan remains uncertainty, never success.
	remaining := st.Size() - s.Boundary.Offset
	if remaining > 32<<20 {
		return Proof{}, false, fmt.Errorf("journal scan exceeds limit")
	}
	reader := bufio.NewReaderSize(io.LimitReader(f, remaining), 64*1024)
	offset := s.Boundary.Offset
	var proof Proof
	matches := 0
	for {
		if err := ctx.Err(); err != nil {
			return Proof{}, false, err
		}
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// Reassemble complete records within the already bounded tail,
			// checking the caller's shared budget between read fragments.
			complete := append([]byte(nil), line...)
			for err == bufio.ErrBufferFull {
				if e := ctx.Err(); e != nil {
					return Proof{}, false, e
				}
				var part []byte
				part, err = reader.ReadSlice('\n')
				complete = append(complete, part...)
			}
			line = complete
		}
		if err == io.EOF {
			break
		} // incomplete tails cannot prove reception
		if err != nil {
			return Proof{}, false, err
		}
		end := offset + int64(len(line))
		var record struct {
			Type, SessionID, UUID string
			Message               struct {
				Role    string
				Content json.RawMessage
			}
		}
		// sessionId differs from session_id on the socket wire.
		var raw struct {
			Type      string          `json:"type"`
			SessionID string          `json:"sessionId"`
			UUID      string          `json:"uuid"`
			Message   json.RawMessage `json:"message"`
		}
		if json.Unmarshal(line, &raw) == nil {
			record.Type, record.SessionID, record.UUID = raw.Type, raw.SessionID, raw.UUID
			if json.Unmarshal(raw.Message, &record.Message) == nil && record.Type == "user" && record.Message.Role == "user" && record.SessionID == s.Target.ClaudeSessionID && record.UUID != "" {
				var text string
				if json.Unmarshal(record.Message.Content, &text) != nil {
					var blocks []struct{ Type, Text string }
					if json.Unmarshal(record.Message.Content, &blocks) == nil && len(blocks) == 1 && blocks[0].Type == "text" {
						text = blocks[0].Text
					}
				}
				// Only a whole exact incoming envelope qualifies. Quoted/assistant/tool
				// or newest-transcript UI matches never participate.
				exact := text == s.Envelope || text == "Another Claude session sent a message:\n"+s.Envelope+"\n\nThis came from another Claude session — not typed by your user, but very likely working on their behalf."
				if s.Envelope != "" && exact && !bytes.Contains(raw.Message, []byte(`"tool_result"`)) {
					matches++
					proof = Proof{RecordID: record.UUID, Offset: offset, End: end}
				}
			}
		}
		offset = end
	}
	return proof, matches == 1, nil
}
