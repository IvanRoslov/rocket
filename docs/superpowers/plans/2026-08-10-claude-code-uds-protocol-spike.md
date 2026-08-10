# Claude Code UDS Messaging Protocol — Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Document the reverse-engineered cross-session messaging (UDS) protocol of Claude Code CLI ≥ 2.1.224 and ship a minimal, tested Go sender that can deliver a prompt into a live Claude Code session without tmux.

**Architecture:** A new leaf package `internal/socketmsg` implements peer discovery (reading `~/.claude/sessions/<pid>.json`) and a newline-delimited-JSON writer over a Unix domain socket. No daemon/store wiring in this spike — the package is standalone and exercised by unit tests plus a tiny `cmd/ccsend` manual E2E driver.

**Tech Stack:** Go 1.25, stdlib only (`net`, `encoding/json`, `os`, `path/filepath`, `crypto/rand` via `github.com/google/uuid` if already vendored — otherwise hand-rolled UUIDv4 from `crypto/rand`).

## Global Constraints

- Target CLI: Claude Code ≥ 2.1.224; verified against 2.1.226 (`GIT_SHA e140b3281c1e8d834468889bd0a5c3fd2f15507c`).
- Wire format: one JSON object per line, `\n`-terminated, UTF-8. Server drops the connection if 1 MiB accumulates without a newline.
- `msgV` is `1`; `msg_id` must match `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`.
- Sender address form: `uds:<percent-encoded socket path>`; allowed literal chars are `A-Za-z0-9:_/.\-`, everything else `%XX` uppercase hex.
- Envelope tag is `cross-session-message`; body must have `</cross-session-message` occurrences escaped as `<\/cross-session-message`.
- Stdlib only; no new go.mod dependencies.
- Doc lives at `docs/design/cc-socket-protocol.md`, in Russian (repo docs language), with verbatim quoted identifiers from the bundle.

---

### Task 1: Design document

**Files:**
- Create: `docs/design/cc-socket-protocol.md`

**Interfaces:**
- Consumes: nothing.
- Produces: the normative reference that Tasks 2–5 implement.

- [ ] **Step 1: Write the document** covering: socket location and permissions, framing, `user` message shape, `control` message shape, the `<cross-session-message>` envelope, sender address encoding, receiver-side admission gating (`crossSessionInbound`), peer-credential handling, receipts (`peer_message_status`), and known limits.

- [ ] **Step 2: Commit**

```bash
git add docs/design/cc-socket-protocol.md
git commit -m "docs: протокол cross-session messaging Claude Code (UDS)"
```

---

### Task 2: Address encoding

**Files:**
- Create: `internal/socketmsg/addr.go`
- Test: `internal/socketmsg/addr_test.go`

**Interfaces:**
- Produces: `func EncodeAddr(socketPath string) string` returning `"uds:" + percentEncoded(socketPath)`.

- [ ] **Step 1: Write the failing test**

```go
func TestEncodeAddr(t *testing.T) {
	if got := EncodeAddr("/tmp/cc-socks/123.sock"); got != "uds:/tmp/cc-socks/123.sock" {
		t.Fatalf("got %q", got)
	}
	if got := EncodeAddr("/tmp/a b.sock"); got != "uds:/tmp/a%20b.sock" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `go test ./internal/socketmsg/` → build failure, `EncodeAddr` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package socketmsg

import (
	"fmt"
	"strings"
)

func EncodeAddr(socketPath string) string {
	var b strings.Builder
	b.WriteString("uds:")
	for _, r := range []byte(socketPath) {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == ':', r == '_', r == '/', r == '.', r == '\\', r == '-':
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes** — `go test ./internal/socketmsg/`

- [ ] **Step 5: Commit**

```bash
git add internal/socketmsg/addr.go internal/socketmsg/addr_test.go
git commit -m "ccsock: кодирование адреса отправителя uds:"
```

---

### Task 3: Envelope

**Files:**
- Create: `internal/socketmsg/envelope.go`
- Test: `internal/socketmsg/envelope.go`'s test `internal/socketmsg/envelope_test.go`

**Interfaces:**
- Consumes: `EncodeAddr` from Task 2.
- Produces: `func Envelope(from, fromName, body string) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestEnvelope(t *testing.T) {
	got := Envelope("uds:/tmp/cc-socks/1.sock", "rocket", "hi")
	want := "<cross-session-message from=\"uds:/tmp/cc-socks/1.sock\" from-name=\"rocket\">\nhi\n</cross-session-message>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnvelopeEscapesCloseTag(t *testing.T) {
	got := Envelope("uds:/x", "", "</cross-session-message>")
	if !strings.Contains(got, `<\/cross-session-message>`) {
		t.Fatalf("close tag not neutralized: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `go test ./internal/socketmsg/` → `Envelope` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package socketmsg

import (
	"regexp"
	"strings"
)

const envelopeTag = "cross-session-message"

var subCloseTag = regexp.MustCompile(`(?i)</(?:` + envelopeTag + `)(?:[>\s/]|$)`)

func Envelope(from, fromName, body string) string {
	var attrs strings.Builder
	if from != "" {
		attrs.WriteString(` from="` + from + `"`)
	}
	if fromName != "" {
		attrs.WriteString(` from-name="` + strings.NewReplacer(`"`, "", "<", "", ">", "").Replace(fromName) + `"`)
	}
	safe := subCloseTag.ReplaceAllStringFunc(body, func(m string) string {
		return `<\/` + m[2:]
	})
	return "<" + envelopeTag + attrs.String() + ">\n" + safe + "\n</" + envelopeTag + ">"
}
```

- [ ] **Step 4: Run test to verify it passes** — `go test ./internal/socketmsg/`

- [ ] **Step 5: Commit**

```bash
git add internal/socketmsg/envelope.go internal/socketmsg/envelope_test.go
git commit -m "ccsock: конверт cross-session-message"
```

---

### Task 4: Peer discovery

**Files:**
- Create: `internal/socketmsg/discover.go`
- Test: `internal/socketmsg/discover_test.go`

**Interfaces:**
- Produces:

```go
type Session struct {
	PID                 int    `json:"pid"`
	SessionID           string `json:"sessionId"`
	CWD                 string `json:"cwd"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	Version             string `json:"version"`
	PeerProtocol        int    `json:"peerProtocol"`
	MessagingSocketPath string `json:"messagingSocketPath"`
	Status              string `json:"status"`
	StartedAt           int64  `json:"startedAt"`
	UpdatedAt           int64  `json:"updatedAt"`
}

func SessionsDir() string
func ListSessions(dir string) ([]Session, error)
func FindBySessionID(dir, sessionID string) (Session, bool, error)
```

`ListSessions` reads only files matching `^\d+\.json$`, skips unparsable entries, and returns entries in filename order.

- [ ] **Step 1: Write the failing test**

```go
func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "42.json"), []byte(`{"pid":42,"sessionId":"s1","messagingSocketPath":"/tmp/cc-socks/42.sock","peerProtocol":1,"kind":"interactive"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "not-a-pid.json"), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(dir, "43.json"), []byte(`{ broken`), 0o600)

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != 42 || got[0].MessagingSocketPath != "/tmp/cc-socks/42.sock" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindBySessionID(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "42.json"), []byte(`{"pid":42,"sessionId":"s1"}`), 0o600)
	s, ok, err := FindBySessionID(dir, "s1")
	if err != nil || !ok || s.PID != 42 {
		t.Fatalf("got %+v ok=%v err=%v", s, ok, err)
	}
	if _, ok, _ := FindBySessionID(dir, "nope"); ok {
		t.Fatal("unexpected hit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `go test ./internal/socketmsg/` → `ListSessions` undefined.

- [ ] **Step 3: Write minimal implementation** — `os.ReadDir`, filter with `regexp.MustCompile(`^\d+\.json$`)`, `json.Unmarshal` into `Session`, skip errors. `SessionsDir()` returns `filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR")|~/.claude, "sessions")`.

- [ ] **Step 4: Run test to verify it passes** — `go test ./internal/socketmsg/`

- [ ] **Step 5: Commit**

```bash
git add internal/socketmsg/discover.go internal/socketmsg/discover_test.go
git commit -m "ccsock: обнаружение живых сессий по ~/.claude/sessions"
```

---

### Task 5: Sender

**Files:**
- Create: `internal/socketmsg/send.go`
- Test: `internal/socketmsg/send_test.go`

**Interfaces:**
- Consumes: `EncodeAddr`, `Envelope`.
- Produces:

```go
type SendOptions struct {
	From     string        // our own socket path, optional
	FromName string        // display name, optional
	Priority string        // "next" (default), "now", "later"
	Timeout  time.Duration // default 5s
}

func Send(socketPath, text string, opt SendOptions) (msgID string, err error)
func SendControl(socketPath string, action map[string]any, timeout time.Duration) error
func Rename(socketPath, name string, timeout time.Duration) error
```

- [ ] **Step 1: Write the failing test** — spin up a `net.Listen("unix", …)` in `t.TempDir()`, call `Send`, read one line, assert the decoded JSON has `type=="user"`, `msgV==1`, valid UUID `msg_id`, `priority=="next"`, and `message.content` wrapped in the envelope.

- [ ] **Step 2: Run test to verify it fails** — `go test ./internal/socketmsg/` → `Send` undefined.

- [ ] **Step 3: Write minimal implementation** — `net.DialTimeout("unix", …)`, marshal, write `payload + "\n"`, `CloseWrite`, close.

- [ ] **Step 4: Run test to verify it passes** — `go test ./internal/socketmsg/`

- [ ] **Step 5: Commit**

```bash
git add internal/socketmsg/send.go internal/socketmsg/send_test.go
git commit -m "ccsock: отправка user/control сообщений в сокет сессии"
```

---

### Task 6: Manual E2E driver

**Files:**
- Create: `cmd/ccsend/main.go`

**Interfaces:**
- Consumes: the whole `ccsock` package.

- [ ] **Step 1: Write the driver** — `ccsend list` prints live sessions; `ccsend send <socket|sessionId> <text>` sends.

- [ ] **Step 2: Verify end-to-end** — `go run ./cmd/ccsend list`, then send to this session's own `$CLAUDE_CODE_MESSAGING_SOCKET` and confirm the prompt arrives.

- [ ] **Step 3: Commit**

```bash
git add cmd/ccsend/main.go
git commit -m "ccsend: ручной драйвер для проверки UDS-протокола"
```
