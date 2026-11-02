package runtime

// Real-tmux content regression for Kimi current-turn steering. It runs a
// deterministic stub TUI (a test double, not Kimi) in an isolated tmux session
// and drives the real tmuxRuntime.Inject through the real capture/send-keys
// path. The stub renders Kimi 2.4.0's composer and queue dialect, parks the
// bracketed paste in its queue on Enter while "busy", and on Ctrl-S records
// the queued payload as the current turn's input. The assertion is about the
// payload reaching the turn — not merely that a key was sent.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const kimiSteerStub = `import os, sys, termios, tty

dir = sys.argv[1]
events = os.path.join(dir, "events.log")
turn_in = os.path.join(dir, "turn-input.txt")
fd = sys.stdin.fileno()
tty.setraw(fd)

draft = ""
queue = None

def log(event):
    with open(events, "a") as f:
        f.write(event + "\n")

def collapse(text):
    return " ".join(text.split())

def draw():
    rows = ["\x1b[2J\x1b[H"]
    if queue is not None:
        rows.append(" " + "─" * 50)
        rows.append("   ❯ " + collapse(queue)[:70])
        rows.append("   ↑ to edit · ctrl-s to steer immediately")
    rows.append(" ╭" + "─" * 50 + "╮")
    rows.append(" │ > " + draft.replace("\n", " ")[:46].ljust(46) + " │")
    rows.append(" ╰" + "─" * 50 + "╯")
    rows.append(" Never Ask  K3.1 Pro thinking: high")
    rows.append(" " + " " * 40 + "context: 0% (0/1M)")
    sys.stdout.write("\r\n".join(rows) + "\r\n")
    sys.stdout.flush()

def enter():
    global draft, queue
    if draft == "":
        return
    queue, draft = draft, ""
    log("enter")
    draw()

def ctrl_s():
    global queue
    log("ctrl-s")
    if queue is not None:
        with open(turn_in, "w") as f:
            f.write(queue)
        queue = None
    draw()

def key(byte):
    global draft
    if byte in (13, 10):
        enter()
    elif byte == 19:
        ctrl_s()
    elif 32 <= byte < 127 or byte >= 128:
        draft += bytes([byte]).decode("utf-8", "replace")
        draw()

sys.stdout.write("\x1b[?2004h")
sys.stdout.flush()
buf = b""
in_paste = False
draw()
while True:
    data = os.read(fd, 65536)
    if not data:
        break
    buf += data
    while True:
        if in_paste:
            end = buf.find(b"\x1b[201~")
            if end < 0:
                break
            pasted = buf[:end].decode("utf-8", "replace").replace("\r\n", "\n").replace("\r", "\n")
            draft += pasted
            buf = buf[end + 6:]
            in_paste = False
            draw()
            continue
        start = buf.find(b"\x1b[200~")
        if start < 0:
            for byte in buf:
                key(byte)
            buf = b""
            break
        for byte in buf[:start]:
            key(byte)
        buf = buf[start + 6:]
        in_paste = True
        log("paste")
`

// TestTmux_Inject_KimiSteerReachesTurn runs the real tmux Inject against the
// stub: the complete multiline message must be what Ctrl-S hands to the
// current turn, through exactly one paste and one steer key.
func TestTmux_Inject_KimiSteerReachesTurn(t *testing.T) {
	requireTmux(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "kimi_steer_stub.py")
	if err := os.WriteFile(stub, []byte(kimiSteerStub), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	rt := NewTmux()
	h, err := rt.Create(ctx, CreateSpec{
		Name:    uniqueName(t, "-steer"),
		Dir:     dir,
		Command: "python3 " + stub + " " + dir,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		return err == nil && strings.Contains(out, "context: 0% (")
	}, "stub Kimi pane to render its composer")

	text := "[from orch] first line\nsecond line\nthird line"
	if err := rt.Inject(ctx, h, text, InjectOpts{Bracketed: true, Dialect: PaneDialectKimi}); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	var steered string
	waitFor(t, func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "turn-input.txt"))
		if err != nil {
			return false
		}
		steered = string(b)
		return steered != ""
	}, "the stub to record the steered current-turn input")
	if steered != text {
		t.Fatalf("current-turn input = %q, want the complete message %q", steered, text)
	}

	events, err := os.ReadFile(filepath.Join(dir, "events.log"))
	if err != nil {
		t.Fatalf("read stub events: %v", err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
		counts[line]++
	}
	if counts["paste"] != 1 || counts["enter"] != 1 || counts["ctrl-s"] != 1 {
		t.Fatalf("stub events = %v, want one paste, one Enter, one Ctrl-S", counts)
	}
}
