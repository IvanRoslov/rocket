package socketmsg

import (
	"io"
	"net"
	"runtime"
	"time"
)

type WriteOutcome string

const (
	NotSent       WriteOutcome = "not_sent"
	PossibleWrite WriteOutcome = "possible_write"
)

// writePayload crosses the uncertainty boundary before calling Write. Even a
// zero-byte write error may follow a kernel-side side effect. Nothing after
// that boundary can authorize a retry; CloseWrite is part of that same send.
func writePayload(c net.Conn, p []byte, timeout time.Duration) (WriteOutcome, error) {
	if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
		return NotSent, err
	}
	n, err := c.Write(p)
	if err != nil {
		return PossibleWrite, err
	}
	if n != len(p) {
		return PossibleWrite, io.ErrShortWrite
	}
	if runtime.GOOS == "darwin" {
		time.Sleep(macOSLingerDelay)
	}
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return PossibleWrite, cw.CloseWrite()
	}
	return PossibleWrite, nil
}
