package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/IvanRoslov/rocket/internal/store"
)

// registerTermRoutes wires the /v1/sessions/{id}/term WebSocket terminal
// route onto mux.
func registerTermRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/sessions/{id}/term", func(w http.ResponseWriter, r *http.Request) {
		handleSessionTerm(w, r, d)
	})
}

// termControl is the JSON shape of a client->server text control frame:
// {"type":"resize","cols":N,"rows":N} or {"type":"ping"}.
type termControl struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// parseControl decodes a text WS frame into a termControl. It returns
// ok=false for malformed JSON, a missing type, or an unrecognized type.
func parseControl(data []byte) (termControl, bool) {
	var c termControl
	if err := json.Unmarshal(data, &c); err != nil {
		return termControl{}, false
	}
	switch c.Type {
	case "resize", "ping":
		return c, true
	default:
		return termControl{}, false
	}
}

// maxTermDim bounds resize control frames: PTY dimensions outside
// [1, maxTermDim] are rejected rather than applied, so a malformed or
// malicious resize can't panic pty.Setsize or exhaust memory without
// killing the connection.
const maxTermDim = 4096

// validResize reports whether cols/rows are in the acceptable range for a
// PTY resize.
func validResize(cols, rows int) bool {
	return cols >= 1 && cols <= maxTermDim && rows >= 1 && rows <= maxTermDim
}

// handleSessionTerm upgrades the connection to a WebSocket and proxies a
// tmux attach session's PTY over it: server->client binary frames carry PTY
// output; client->server binary frames carry input; client->server text
// frames carry JSON control messages (resize/ping). Closing the WS kills
// only the attach client process, never the underlying tmux session.
func handleSessionTerm(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	sess, err := d.Store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if sess.State != "spawning" && sess.State != "running" {
		writeErr(w, http.StatusConflict, "session_not_live", "session is not live: "+sess.State)
		return
	}

	argv, err := d.Manager.AttachCommand(id)
	if err != nil {
		writeManagerErr(w, err)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Default read limit is 32KiB, which is far too small for a large paste
	// into the terminal; a paste bigger than that kills the connection.
	conn.SetReadLimit(1 << 20)

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "failed to start attach")
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	readonly := r.URL.Query().Get("readonly") == "true"
	ctx := r.Context()

	// pty -> ws: streams attach client output to the browser. Exits when
	// the PTY read errors out (attach process died) or the write to the ws
	// fails (connection closed).
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				conn.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
		}
	}()

	// ws -> pty: forwards input/control frames from the browser. Exits
	// when the ws read errors out (connection closed by either side),
	// which also unblocks the pty->ws goroutine via the deferred close
	// above.
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if !readonly {
				_, _ = ptmx.Write(data)
			}
		case websocket.MessageText:
			if c, ok := parseControl(data); ok {
				switch c.Type {
				case "resize":
					// Readonly clients (view-only observers) must not resize
					// the shared PTY out from under the writer; treat resize
					// like input and ignore it. Out-of-range dimensions are
					// also ignored rather than killing the connection.
					if !readonly && validResize(c.Cols, c.Rows) {
						_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(c.Cols), Rows: uint16(c.Rows)})
					}
				case "ping":
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				}
			}
		}
	}
}
