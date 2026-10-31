package nativeinput_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/nativeinput"
)

func TestSendAccepted(t *testing.T) {
	target := testTarget(t)
	requestSeen := make(chan map[string]any, 1)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		var req map[string]any
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requestSeen <- req
		fmt.Fprintf(conn, `{"v":1,"session_id":"sess","instance_id":"launch","message_id":42,"outcome":"accepted"}`+"\n")
	})

	outcome, err := nativeinput.Send(context.Background(), target, 42, "привет\nмир")
	if err != nil || outcome != nativeinput.Accepted {
		t.Fatalf("Send = %v, %v; want accepted, nil", outcome, err)
	}
	req := <-requestSeen
	if req["v"] != float64(1) || req["session_id"] != "sess" || req["instance_id"] != "launch" || req["message_id"] != float64(42) || req["text"] != "привет\nмир" {
		t.Fatalf("request = %#v", req)
	}
}

func TestSendNotSentResponse(t *testing.T) {
	target := testTarget(t)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		fmt.Fprintf(conn, `{"v":1,"session_id":"sess","instance_id":"launch","message_id":42,"outcome":"not_sent"}`+"\n")
	})

	outcome, err := nativeinput.Send(context.Background(), target, 42, "text")
	if err != nil || outcome != nativeinput.NotSent {
		t.Fatalf("Send = %v, %v; want not_sent, nil", outcome, err)
	}
}

func TestSendDialFailureNotSent(t *testing.T) {
	target := testTarget(t)
	outcome, err := nativeinput.Send(context.Background(), target, 42, "text")
	if err == nil || outcome != nativeinput.NotSent {
		t.Fatalf("Send = %v, %v; want not_sent and dial error", outcome, err)
	}
}

func TestSendDisconnectAfterWriteUnknown(t *testing.T) {
	target := testTarget(t)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
	})

	outcome, err := nativeinput.Send(context.Background(), target, 42, "text")
	if err == nil || outcome != nativeinput.Unknown {
		t.Fatalf("Send = %v, %v; want unknown and response error", outcome, err)
	}
}

func TestSendRejectsOversizedRequest(t *testing.T) {
	target := testTarget(t)
	outcome, err := nativeinput.Send(context.Background(), target, 42, strings.Repeat("x", 1<<20))
	if err == nil || outcome != nativeinput.NotSent {
		t.Fatalf("Send = %v, %v; want not_sent and frame-size error", outcome, err)
	}
	if !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("error = %v, want frame limit", err)
	}
}

func TestSendRejectsOversizedResponse(t *testing.T) {
	target := testTarget(t)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outcome, err := nativeinput.Send(ctx, target, 42, "text")
	if err == nil || outcome != nativeinput.Unknown {
		t.Fatalf("Send = %v, %v; want unknown and response-size error", outcome, err)
	}
}

func TestSendRejectsMalformedResponse(t *testing.T) {
	target := testTarget(t)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		fmt.Fprintln(conn, "not-json")
	})

	outcome, err := nativeinput.Send(context.Background(), target, 42, "text")
	if err == nil || outcome != nativeinput.Unknown {
		t.Fatalf("Send = %v, %v; want unknown and JSON error", outcome, err)
	}
}

func TestSendRejectsWrongResponseIdentity(t *testing.T) {
	target := testTarget(t)
	target.SocketPath = serveOnce(t, func(conn net.Conn) {
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		fmt.Fprintf(conn, `{"v":1,"session_id":"sess","instance_id":"other-launch","message_id":42,"outcome":"accepted"}`+"\n")
	})

	outcome, err := nativeinput.Send(context.Background(), target, 42, "text")
	if err == nil || outcome != nativeinput.Unknown {
		t.Fatalf("Send = %v, %v; want unknown and identity error", outcome, err)
	}
}

func testTarget(t *testing.T) nativeinput.Target {
	t.Helper()
	return nativeinput.Target{
		SessionID:  "sess",
		InstanceID: "launch",
		SocketPath: filepath.Join(t.TempDir(), "missing.sock"),
	}
}

func serveOnce(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ni")
	if err != nil {
		t.Fatalf("make short socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "n.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		handler(conn)
		_ = conn.Close()
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return path
}
