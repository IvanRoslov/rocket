package socketmsg

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type scriptedConn struct {
	net.Conn
	n                  int
	writeErr, closeErr error
	writes             int
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.writes++
	if c.n < 0 {
		return len(p), c.writeErr
	}
	return c.n, c.writeErr
}
func (c *scriptedConn) CloseWrite() error           { return c.closeErr }
func (c *scriptedConn) SetDeadline(time.Time) error { return nil }
func (c *scriptedConn) Close() error                { return nil }
func TestWriteEvidenceNeverAuthorizesResend(t *testing.T) {
	for _, tc := range []struct {
		name               string
		n                  int
		writeErr, closeErr error
	}{
		{"partial", 2, io.ErrUnexpectedEOF, nil}, {"short write", 2, nil, nil}, {"full", -1, nil, nil}, {"zero error", 0, io.ErrClosedPipe, nil}, {"CloseWrite", -1, nil, errors.New("shutdown failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &scriptedConn{n: tc.n, writeErr: tc.writeErr, closeErr: tc.closeErr}
			out, _ := writePayload(c, []byte("frame\n"), time.Millisecond)
			if out != PossibleWrite || c.writes != 1 {
				t.Fatalf("outcome=%s writes=%d", out, c.writes)
			}
		})
	}
}
