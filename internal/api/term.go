package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"github.com/IvanRoslov/rocket/internal/store"
)

// termClaims refcounts, per session, the writer terminal connections that
// have pinned the tmux window to their size (Manager.PinWindowSize). The
// window must stay pinned while ANY writer web terminal is attached (two
// dashboard tabs on the same session must not unpin each other), and be
// released only when the last one disconnects.
type termClaims struct {
	mu     sync.Mutex
	counts map[string]int
}

func newTermClaims() *termClaims {
	return &termClaims{counts: make(map[string]int)}
}

// claim registers one writer connection for the session.
func (c *termClaims) claim(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[id]++
}

// release unregisters one writer connection and reports whether it was
// the last one (i.e. the caller should unpin the window).
func (c *termClaims) release(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.counts[id] - 1
	if n <= 0 {
		delete(c.counts, id)
		return true
	}
	c.counts[id] = n
	return false
}

// registerTermRoutes wires the /v1/sessions/{id}/term WebSocket terminal
// route onto mux.
func registerTermRoutes(mux *http.ServeMux, d Deps) {
	claims := newTermClaims()
	mux.HandleFunc("GET /v1/sessions/{id}/term", func(w http.ResponseWriter, r *http.Request) {
		handleSessionTerm(w, r, d, claims)
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
//
// Client-size policy (docs/03-daemon-api.md «Размер окна»): the web
// terminal is the primary surface, so on every resize control frame from
// a writer connection the daemon both resizes the attach PTY and PINS the
// tmux window to that exact size (window-size manual). Differently-sized
// local clients get a cropped/padded view instead of corrupting the web
// one. When the last writer web terminal disconnects, the pin is released
// and the window follows local clients again ("latest").
func handleSessionTerm(w http.ResponseWriter, r *http.Request, d Deps, claims *termClaims) {
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

	// Writer connections pin the tmux window to their size (see the
	// handler doc comment). Claim/release brackets the whole connection
	// so a second tab can't be unpinned by the first one closing; the
	// actual pin happens on each resize frame below. Unpin runs on a
	// fresh context: r.Context() is already canceled by the time the
	// client has disconnected.
	if !readonly {
		claims.claim(id)
		defer func() {
			if claims.release(id) {
				unpinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = d.Manager.UnpinWindowSize(unpinCtx, id)
			}
		}()
	}

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
						// Pin the tmux window to this client's size so the
						// web view always renders full-width regardless of
						// other (e.g. wider local) clients; see the handler
						// doc comment for the policy. Best-effort: a failed
						// pin degrades to tmux's own sizing rather than
						// killing the terminal.
						_ = d.Manager.PinWindowSize(ctx, id, c.Cols, c.Rows)
					}
				case "ping":
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				}
			}
		}
	}
}
