package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

type termScrollRuntime struct {
	sessFakeRuntime
	mu        sync.Mutex
	calls     []string
	scrollErr error
	exitErr   error
}

func (f *termScrollRuntime) AttachCommand(runtime.Handle) []string {
	return []string{"cat"}
}

func (f *termScrollRuntime) ScrollHistory(_ context.Context, _ runtime.Handle, lines int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "scroll:"+strconv.Itoa(lines))
	return f.scrollErr
}

func (f *termScrollRuntime) ExitHistory(context.Context, runtime.Handle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "exit")
	return f.exitErr
}

func (f *termScrollRuntime) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func termScrollConn(t *testing.T, rt runtime.Runtime, tmuxName string, readonly bool) (*websocket.Conn, context.Context) {
	t.Helper()
	d := sessionsTestDeps(t)
	d.Manager = session.NewManager(d.Store, d.Bus, rt, sessFakeWorkspace{}, d.Cfg)
	addTestRepo(t, d, "termrepo")
	if err := d.Store.AddProject(store.Project{ID: "termproj", Name: "termproj", MainRepo: "termrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := d.Store.AddSession(store.Session{
		ID: "term-scroll", Kind: "worker", ProjectID: "termproj", RepoID: "termrepo",
		FeatureSlug: "term", Agent: "fake", Branch: "feature/term/scroll",
		WorktreePath: "/fake/wt/term-scroll", TmuxName: tmuxName, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	srv := newTestServer(t, d)
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + "/v1/sessions/term-scroll/term"
	if readonly {
		url += "?readonly=true"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, ctx
}

func sendTermAndWait(t *testing.T, conn *websocket.Conn, ctx context.Context, frames ...struct {
	typ  websocket.MessageType
	data []byte
}) {
	t.Helper()
	for _, frame := range frames {
		if err := conn.Write(ctx, frame.typ, frame.data); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read pong: %v", err)
		}
		if typ == websocket.MessageText && string(data) == `{"type":"pong"}` {
			return
		}
	}
}

func termFrame(typ websocket.MessageType, data string) struct {
	typ  websocket.MessageType
	data []byte
} {
	return struct {
		typ  websocket.MessageType
		data []byte
	}{typ, []byte(data)}
}

func TestSessionTermScrollThenInputExitsHistory(t *testing.T) {
	f := &termScrollRuntime{}
	conn, ctx := termScrollConn(t, f, "rocket-term-scroll", false)
	paste := "qg/" + strings.Repeat("x", 40*1024) + "\n"
	sendTermAndWait(t, conn, ctx,
		termFrame(websocket.MessageText, `{"type":"scroll","lines":-3}`),
		termFrame(websocket.MessageBinary, paste),
	)
	if got := f.snapshot(); !slices.Equal(got, []string{"scroll:-3", "exit"}) {
		t.Fatalf("runtime calls = %v, want scroll then exit", got)
	}
}

func TestSessionTermScrollOnlyExitsAfterScroll(t *testing.T) {
	f := &termScrollRuntime{}
	conn, ctx := termScrollConn(t, f, "rocket-term-scroll", false)
	sendTermAndWait(t, conn, ctx, termFrame(websocket.MessageBinary, "x\n"))
	if got := f.snapshot(); len(got) != 0 {
		t.Fatalf("runtime calls without scroll = %v, want none", got)
	}
}

func TestSessionTermScrollReadonlyAndZeroIgnored(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readonly bool
		frame    string
	}{
		{"readonly", true, `{"type":"scroll","lines":-3}`},
		{"zero", false, `{"type":"scroll","lines":0}`},
		{"missing lines", false, `{"type":"scroll"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &termScrollRuntime{}
			conn, ctx := termScrollConn(t, f, "rocket-term-scroll", tc.readonly)
			sendTermAndWait(t, conn, ctx,
				termFrame(websocket.MessageText, tc.frame),
				termFrame(websocket.MessageBinary, "x\n"),
			)
			if got := f.snapshot(); len(got) != 0 {
				t.Fatalf("runtime calls = %v, want none", got)
			}
		})
	}
}

func TestSessionTermScrollClampsAndKeepsConnectionOnErrors(t *testing.T) {
	f := &termScrollRuntime{scrollErr: errors.New("scroll failed"), exitErr: errors.New("exit failed")}
	conn, ctx := termScrollConn(t, f, "rocket-term-scroll", false)
	sendTermAndWait(t, conn, ctx,
		termFrame(websocket.MessageText, `{"type":"scroll","lines":-5000}`),
		termFrame(websocket.MessageBinary, "x\n"),
		termFrame(websocket.MessageText, `{"type":"scroll","lines":5000}`),
	)
	if got := f.snapshot(); !slices.Equal(got, []string{"scroll:-1000", "exit", "scroll:1000"}) {
		t.Fatalf("runtime calls = %v, want clamped scroll calls and exit", got)
	}
}

func TestSessionTermScrollFirstPasteReachesRealTmuxPane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ctx := context.Background()
	rt := runtime.NewTmux()
	h, err := rt.Create(ctx, runtime.CreateSpec{
		Name: fmt.Sprintf("rocket-test-%x-scroll", time.Now().UnixNano()),
		Dir:  t.TempDir(),
		// Raw mode lets cat receive the whole paste instead of hitting the
		// terminal's canonical input line limit.
		Command: "seq 1 500; stty -echo -icanon; echo ROCKET_READY; exec cat",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = rt.Destroy(context.Background(), h) })

	deadline := time.Now().Add(3 * time.Second)
	for {
		out, err := rt.Capture(ctx, h, 20)
		if err == nil && strings.Contains(out, "ROCKET_READY") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pane output: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	conn, wsCtx := termScrollConn(t, rt, h.Name, false)
	if err := conn.Write(wsCtx, websocket.MessageText, []byte(`{"type":"scroll","lines":-10}`)); err != nil {
		t.Fatalf("write scroll: %v", err)
	}
	paneMode := func() string {
		t.Helper()
		out, err := exec.CommandContext(wsCtx, "tmux", "display-message", "-p", "-t", "="+h.Name+":", "#{pane_in_mode}").Output()
		if err != nil {
			t.Fatalf("query pane mode: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	deadline = time.Now().Add(3 * time.Second)
	for paneMode() != "1" {
		if time.Now().After(deadline) {
			t.Fatal("scroll frame did not enter copy-mode")
		}
		time.Sleep(20 * time.Millisecond)
	}
	prefix := "qg/SCROLL-INPUT-"
	suffix := "-PASTE-END"
	paste := prefix + strings.Repeat("z", 1024) + suffix + "\n"
	if err := conn.Write(wsCtx, websocket.MessageBinary, []byte(paste)); err != nil {
		t.Fatalf("write paste: %v", err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for {
		out, err := rt.Capture(ctx, h, 100)
		if err == nil && strings.Contains(out, prefix) && strings.Contains(out, suffix) && paneMode() == "0" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("first paste did not reach pane intact; capture=%q, err=%v", out, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestParseControlResize(t *testing.T) {
	c, ok := parseControl([]byte(`{"type":"resize","cols":100,"rows":40}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if c.Type != "resize" || c.Cols != 100 || c.Rows != 40 {
		t.Errorf("got %+v", c)
	}
}

func TestParseControlPing(t *testing.T) {
	c, ok := parseControl([]byte(`{"type":"ping"}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if c.Type != "ping" {
		t.Errorf("got %+v", c)
	}
}

func TestParseControlScroll(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame string
		lines int
	}{
		{"up", `{"type":"scroll","lines":-3}`, -3},
		{"zero", `{"type":"scroll","lines":0}`, 0},
		{"missing lines", `{"type":"scroll"}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := parseControl([]byte(tc.frame))
			if !ok || c.Type != "scroll" || c.Lines != tc.lines {
				t.Fatalf("parseControl(%s) = %+v, %v; want scroll lines=%d", tc.frame, c, ok, tc.lines)
			}
		})
	}
}

func TestClampScroll(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-5000, -1000}, {5000, 1000}, {-3, -3}, {7, 7}, {0, 0},
	} {
		if got := clampScroll(tc.in); got != tc.want {
			t.Errorf("clampScroll(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseControlGarbage(t *testing.T) {
	if _, ok := parseControl([]byte(`not json`)); ok {
		t.Fatalf("expected ok=false for garbage")
	}
	if _, ok := parseControl([]byte(`{}`)); ok {
		t.Fatalf("expected ok=false for missing type")
	}
	if _, ok := parseControl([]byte(`{"type":"bogus"}`)); ok {
		t.Fatalf("expected ok=false for unknown type")
	}
}

func TestValidResizeBounds(t *testing.T) {
	cases := []struct {
		name       string
		cols, rows int
		want       bool
	}{
		{"min valid", 1, 1, true},
		{"max valid", 4096, 4096, true},
		{"typical", 100, 40, true},
		{"zero cols", 0, 40, false},
		{"zero rows", 100, 0, false},
		{"negative cols", -1, 40, false},
		{"negative rows", 100, -1, false},
		{"cols too large", 4097, 40, false},
		{"rows too large", 100, 4097, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validResize(tc.cols, tc.rows); got != tc.want {
				t.Errorf("validResize(%d, %d) = %v, want %v", tc.cols, tc.rows, got, tc.want)
			}
		})
	}
}

func TestParseControlResizeOutOfBounds(t *testing.T) {
	// parseControl only validates JSON shape/type; bounds checking is done
	// separately via validResize so out-of-range resize frames still parse
	// ok=true and are rejected downstream instead of killing the connection.
	c, ok := parseControl([]byte(`{"type":"resize","cols":-1,"rows":999999}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if validResize(c.Cols, c.Rows) {
		t.Errorf("expected validResize to reject cols=%d rows=%d", c.Cols, c.Rows)
	}
}

func TestSessionTermUnknownSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/sessions/nope/term")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestSessionTermDeadSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := d.Store.AddSession(store.Session{
		ID:           "dead-sess",
		Kind:         "worker",
		ProjectID:    "myproj",
		RepoID:       "myrepo",
		FeatureSlug:  "dead",
		Agent:        "fake",
		Branch:       "feature/dead/dead",
		WorktreePath: "/fake/wt/dead-sess",
		TmuxName:     "rocket-dead-sess",
		State:        "killed",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/sessions/dead-sess/term")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_live" {
		t.Errorf("code = %q, want session_not_live", eb.Error.Code)
	}
}

// TestTermClaimsRefcount verifies the pin-refcount semantics: with two
// writer terminals on the same session, closing one must NOT release the
// window pin (the survivor still owns the size); only the last close
// releases. Sessions are counted independently.
func TestTermClaimsRefcount(t *testing.T) {
	c := newTermClaims()

	c.claim("a")
	c.claim("a")
	c.claim("b")

	if c.release("a") {
		t.Fatalf("first of two releases for 'a' must not be last")
	}
	if !c.release("a") {
		t.Fatalf("second release for 'a' must be last")
	}
	if !c.release("b") {
		t.Fatalf("sole release for 'b' must be last")
	}
	// Releasing beyond zero (defensive) still reports last and does not
	// underflow into negative counts.
	if !c.release("a") {
		t.Fatalf("release of unclaimed session must report last")
	}
	c.claim("a")
	if !c.release("a") {
		t.Fatalf("claim after over-release must behave normally")
	}
}
