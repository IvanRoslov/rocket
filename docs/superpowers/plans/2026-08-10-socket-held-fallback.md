# Socket held-receipt fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop losing messages that a bypass-permissions Claude Code recipient *holds* instead of accepting — detect the `held` receipt and fall back to tmux injection instead of reporting the message delivered.

**Architecture:** rocket starts listening on its own unix socket inside the same directory as the recipient's socket (the CLI only sends receipts to a `*.sock` path in its own directory — protocol doc §7). `ClaudeSocketSender` now advertises that address in the message's `from` field, registers a waiter keyed by `msg_id` before writing, and waits a short window for a `peer_message_status` control frame. `held` turns into `ErrHeld`, which the existing `attemptDelivery` fallback already routes to tmux injection in the same attempt. Silence means accept — the protocol never acks an accepted message.

**Tech Stack:** Go, `net` unix sockets, existing `internal/socketmsg` + `internal/queue`.

## Global Constraints

- Never assert `from-mode` (protocol doc §5.2): claiming `bypass` would sneak our messages past a bypass recipient's guard with privileges rocket has no business claiming. The whole point of this task is to handle the hold, not to dodge it.
- rocket's own socket MUST live in the same directory as the recipient's socket and MUST end in `.sock`, else the CLI silently skips the receipt (§7).
- `sun_path` is 103 bytes: socket paths must stay short. Tests must use `shortTempDir`, not `t.TempDir()` (see `internal/queue/socket_test.go`).
- Comments in Go code are in English (matches `internal/queue`); `internal/socketmsg` comments are in Russian (matches that package). Follow the package you are editing.
- No new config keys. The wait window is a constant, overridable per-sender for tests.
- Degrade softly: a receipt listener that cannot be created must not break delivery — the sender falls back to the current "write and hope" behavior.

---

### Task 1: Receipt listener in `internal/socketmsg`

**Files:**
- Create: `internal/socketmsg/receipts.go`
- Create: `internal/socketmsg/receipts_test.go`
- Modify: `internal/socketmsg/send.go` (add `Options.MsgID`, export `NewMsgID`)

**Interfaces:**
- Consumes: `Message`, `EncodeAddr`, `newUUID` from the same package.
- Produces:
  - `func NewMsgID() (string, error)`
  - `Options.MsgID string` — when non-empty, `Send` uses it instead of generating one.
  - `type Receipts struct{...}`
  - `func NewReceipts(prefix string) *Receipts`
  - `func (r *Receipts) Addr(dir string) (string, error)` — path of rocket's socket inside `dir`, created on first call.
  - `func (r *Receipts) Watch(msgID string) (<-chan Message, func())` — buffered(1) channel of receipts for that id, plus a cancel func.
  - `func (r *Receipts) Close() error`

- [ ] **Step 1: Write the failing test** (`receipts_test.go`)

```go
package socketmsg

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "rr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// Квитанция, пришедшая на сокет rocket, должна попасть ровно тому, кто ждёт
// её orig_msg_id.
func TestReceiptsRoutesByOrigMsgID(t *testing.T) {
	r := NewReceipts("rocket-test")
	defer r.Close()

	dir := shortDir(t)
	addr, err := r.Addr(dir)
	if err != nil {
		t.Fatalf("Addr: %v", err)
	}
	if filepath.Dir(addr) != dir || !strings.HasSuffix(addr, ".sock") {
		t.Fatalf("addr = %q, want <dir>/*.sock in %s", addr, dir)
	}

	ch, cancel := r.Watch("abc")
	defer cancel()
	other, cancelOther := r.Watch("zzz")
	defer cancelOther()

	writeFrame(t, addr, Message{
		Type: "control", Action: "peer_message_status",
		Status: "held", OrigMsgID: "abc",
	})

	select {
	case m := <-ch:
		if m.Status != "held" {
			t.Fatalf("status = %q, want held", m.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("квитанция не доехала до ожидающего")
	}
	select {
	case m := <-other:
		t.Fatalf("чужая квитанция попала не туда: %+v", m)
	default:
	}
}

// Второй Addr на тот же каталог не поднимает второй сокет.
func TestReceiptsAddrIsIdempotentPerDir(t *testing.T) {
	r := NewReceipts("rocket-test")
	defer r.Close()
	dir := shortDir(t)
	a, err := r.Addr(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Addr(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("Addr вернул разные пути: %q и %q", a, b)
	}
}

// Close снимает сокет с диска: иначе следующий запуск демона упрёрся бы в
// живой файл, а мёртвый сокет ещё и притворяется адресуемым.
func TestReceiptsCloseRemovesSocket(t *testing.T) {
	r := NewReceipts("rocket-test")
	dir := shortDir(t)
	addr, err := r.Addr(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(addr); !os.IsNotExist(err) {
		t.Fatalf("сокет %s остался после Close (err=%v)", addr, err)
	}
}

// Watch без пришедшей квитанции не течёт: cancel снимает ожидающего.
func TestReceiptsCancelDropsWaiter(t *testing.T) {
	r := NewReceipts("rocket-test")
	defer r.Close()
	_, cancel := r.Watch("abc")
	cancel()
	if n := r.waiterCount(); n != 0 {
		t.Fatalf("осталось %d ожидающих, ждали 0", n)
	}
}

func writeFrame(t *testing.T, path string, m Message) {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	defer conn.Close()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/socketmsg/ -run TestReceipts -v`
Expected: FAIL — `undefined: NewReceipts`.

- [ ] **Step 3: Implement `receipts.go`**

```go
package socketmsg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// maxFrame — тот же предел строки, что и у приёмника CLI (§3): 1 MiB без
// перевода строки означает, что на том конце не наш протокол.
const maxFrame = 1 << 20

// Receipts — обратный канал: сокеты, на которые Claude Code шлёт квитанции
// peer_message_status о судьбе придержанных сообщений (§7).
//
// Квитанция уходит, только если обратный адрес лежит в том же каталоге, что и
// сокет получателя, и оканчивается на .sock. Каталог у получателей в принципе
// может отличаться (XDG_RUNTIME_DIR против /tmp/cc-socks-<uid>), поэтому
// слушатель поднимается лениво на каждый встреченный каталог.
type Receipts struct {
	prefix string

	mu        sync.Mutex
	closed    bool
	listeners map[string]net.Listener // каталог -> слушатель
	paths     map[string]string       // каталог -> путь нашего сокета
	waiters   map[string]chan Message // orig_msg_id -> ожидающий
}

// NewReceipts создаёт обратный канал. prefix — начало имени файла сокета;
// к нему добавляется pid, чтобы два демона не дрались за один путь.
func NewReceipts(prefix string) *Receipts {
	return &Receipts{
		prefix:    prefix,
		listeners: make(map[string]net.Listener),
		paths:     make(map[string]string),
		waiters:   make(map[string]chan Message),
	}
}

// Addr возвращает путь к нашему сокету в каталоге dir, поднимая слушатель при
// первом обращении.
func (r *Receipts) Addr(dir string) (string, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", fmt.Errorf("socketmsg: обратный канал закрыт")
	}
	if p, ok := r.paths[dir]; ok {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	path := filepath.Join(dir, fmt.Sprintf("%s-%d.sock", r.prefix, os.Getpid()))
	// Мёртвый сокет от прошлого запуска остаётся на диске и мешает bind.
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", fmt.Errorf("socketmsg: слушатель квитанций %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = ln.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("socketmsg: обратный канал закрыт")
	}
	if p, ok := r.paths[dir]; ok { // гонка двух Addr на один каталог
		r.mu.Unlock()
		_ = ln.Close()
		_ = os.Remove(path)
		return p, nil
	}
	r.listeners[dir] = ln
	r.paths[dir] = path
	r.mu.Unlock()

	go r.accept(ln)
	return path, nil
}

// Watch регистрирует ожидание квитанции по msgID. Канал буферизован, так что
// доставка квитанции никогда не блокирует читателя сокета. Возвращённую
// функцию отмены обязательно вызывать — иначе ожидающий останется в карте.
func (r *Receipts) Watch(msgID string) (<-chan Message, func()) {
	ch := make(chan Message, 1)
	r.mu.Lock()
	r.waiters[msgID] = ch
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.waiters, msgID)
		r.mu.Unlock()
	}
}

// Close снимает все слушатели и удаляет их сокеты с диска.
func (r *Receipts) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	lns := r.listeners
	paths := r.paths
	r.listeners = make(map[string]net.Listener)
	r.paths = make(map[string]string)
	r.mu.Unlock()

	for dir, ln := range lns {
		_ = ln.Close()
		_ = os.Remove(paths[dir])
	}
	return nil
}

func (r *Receipts) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // слушатель закрыт
		}
		go r.read(conn)
	}
}

func (r *Receipts) read(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), maxFrame)
	for sc.Scan() {
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue // не-JSON строку CLI тоже просто пропускает (§3)
		}
		r.dispatch(m)
	}
}

func (r *Receipts) dispatch(m Message) {
	if m.OrigMsgID == "" {
		return
	}
	r.mu.Lock()
	ch, ok := r.waiters[m.OrigMsgID]
	r.mu.Unlock()
	if !ok {
		// Ожидающего нет: квитанция опоздала (denied/expired приходят много
		// позже окна ожидания). Судьбу такого сообщения решать уже нечем —
		// логируем, чтобы это было видно в журнале демона.
		slog.Info("socketmsg: квитанция без ожидающего",
			"status", m.Status, "orig_msg_id", m.OrigMsgID)
		return
	}
	select {
	case ch <- m:
	default: // одну квитанцию уже положили — больше не нужно
	}
}

// waiterCount — для тестов: сколько ожидающих зарегистрировано сейчас.
func (r *Receipts) waiterCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.waiters)
}
```

- [ ] **Step 4: Add `MsgID` to `Options` and export `NewMsgID` in `send.go`**

In the `Options` struct add:

```go
	// MsgID — идентификатор сообщения. Пустая строка означает «сгенерировать».
	// Задавать его нужно, чтобы зарегистрировать ожидание квитанции ДО записи
	// в сокет: held приходит немедленно и может обогнать возврат из Send.
	MsgID string
```

In `Send`, replace `id, err := newUUID()` with:

```go
	id := opt.MsgID
	if id == "" {
		var err error
		if id, err = newUUID(); err != nil {
			return "", err
		}
	}
```

And add:

```go
// NewMsgID возвращает msg_id в формате, который принимает CLI (UUIDv4).
func NewMsgID() (string, error) { return newUUID() }
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/socketmsg/ -v`
Expected: PASS (all, including the pre-existing ones).

- [ ] **Step 6: Commit**

```bash
git add internal/socketmsg/
git commit -m "socketmsg: обратный канал для квитанций peer_message_status"
```

---

### Task 2: `ClaudeSocketSender` waits for a `held` receipt

**Files:**
- Modify: `internal/queue/socket.go`
- Modify: `internal/queue/socket_test.go`

**Interfaces:**
- Consumes: `socketmsg.Receipts`, `socketmsg.NewMsgID`, `Options.MsgID`, `Options.From` from Task 1.
- Produces:
  - `func (s *ClaudeSocketSender) SetReceipts(r *socketmsg.Receipts)`
  - `var ErrHeld error` — the recipient held the message for user approval.
  - unexported field `heldWait time.Duration` (default `defaultHeldWait = 2 * time.Second`) so tests can shorten the window.

- [ ] **Step 1: Write the failing tests** (append to `internal/queue/socket_test.go`)

```go
// heldSocket — приёмник, который на каждое user-сообщение отвечает квитанцией
// held на обратный адрес из поля from. Ровно так ведёт себя сессия в
// bypassPermissions без crossSessionInbound=accept (протокол §6).
func heldSocket(t *testing.T, dir, name string) string {
	t.Helper()
	return replySocket(t, dir, name, "held")
}

func replySocket(t *testing.T, dir, name, status string) string {
	t.Helper()
	path := filepath.Join(dir, name+".sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					var m socketmsg.Message
					if json.Unmarshal(sc.Bytes(), &m) != nil || m.From == "" {
						continue
					}
					reply, err := net.Dial("unix", decodeAddrForTest(m.From))
					if err != nil {
						return
					}
					raw, _ := json.Marshal(socketmsg.Message{
						MsgV: 1, Type: "control", Action: "peer_message_status",
						Status: status, OrigMsgID: m.MsgID,
					})
					_, _ = reply.Write(append(raw, '\n'))
					_ = reply.Close()
				}
			}()
		}
	}()
	return path
}

// decodeAddrForTest обращает socketmsg.EncodeAddr: снимает схему uds: и
// раскодирует %XX.
func decodeAddrForTest(addr string) string {
	s := strings.TrimPrefix(addr, socketmsg.AddrScheme)
	out, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return out
}

// Придержанное сообщение — не доставленное. Send обязан сказать об этом
// ошибкой, иначе очередь пометит его delivered, а получатель никогда его не
// увидит: hold-буфер молча протухает.
func TestSendReportsHeld(t *testing.T) {
	sockDir := shortTempDir(t)
	regDir := t.TempDir()
	path := heldSocket(t, sockDir, "999")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 999, SessionID: "sid-999", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	r := socketmsg.NewReceipts("rocket-test")
	defer r.Close()
	s := NewClaudeSocketSender(regDir)
	s.SetReceipts(r)
	s.heldWait = 3 * time.Second

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	err := s.Send(sess, "orch", "текст")
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("Send err = %v, want ErrHeld", err)
	}
}

// Принятое сообщение квитанции не порождает никогда (§7). Тишина в окне —
// это успех, а не таймаут-ошибка.
func TestSendSucceedsOnSilence(t *testing.T) {
	sockDir := shortTempDir(t)
	regDir := t.TempDir()
	path := liveSocket(t, sockDir, "998")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 998, SessionID: "sid-998", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	r := socketmsg.NewReceipts("rocket-test")
	defer r.Close()
	s := NewClaudeSocketSender(regDir)
	s.SetReceipts(r)
	s.heldWait = 200 * time.Millisecond

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	if err := s.Send(sess, "orch", "текст"); err != nil {
		t.Fatalf("Send = %v, want nil при отсутствии квитанции", err)
	}
}

// Без обратного канала поведение прежнее: пишем и не ждём ничего.
func TestSendWithoutReceiptsDoesNotWait(t *testing.T) {
	sockDir := shortTempDir(t)
	regDir := t.TempDir()
	path := heldSocket(t, sockDir, "997")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 997, SessionID: "sid-997", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	s := NewClaudeSocketSender(regDir)
	s.heldWait = time.Hour // должен быть неиспользован

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	start := time.Now()
	if err := s.Send(sess, "orch", "текст"); err != nil {
		t.Fatalf("Send = %v, want nil", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Send ждал %v без обратного канала", d)
	}
}
```

Add the imports the new tests need to the file's import block: `bufio`, `errors`, `net/url`, `strings`, `time`.

- [ ] **Step 2: Run and watch it fail**

Run: `go test ./internal/queue/ -run 'TestSend(ReportsHeld|SucceedsOnSilence|WithoutReceipts)' -v`
Expected: FAIL — `s.SetReceipts undefined`, `ErrHeld undefined`, `s.heldWait undefined`.

- [ ] **Step 3: Implement in `internal/queue/socket.go`**

Add imports `log/slog`, `path/filepath`, `time`.

Add next to `ErrNoSocket`:

```go
// ErrHeld reports that the recipient accepted the bytes but did NOT queue the
// message: it sits in their hold buffer awaiting the user's approval, and if
// nobody approves it, it expires silently (protocol doc §6). That is a
// non-delivery, so the queue must treat it exactly like a socket failure and
// fall back to tmux injection.
var ErrHeld = errors.New("queue: recipient held the message for user approval")

// defaultHeldWait is how long Send waits for a peer_message_status receipt
// after the write. The recipient emits `held` immediately from its inbound
// handler, so this only has to cover a local UDS round trip plus a busy Node
// event loop. Silence means accept: an accepted message never produces a
// receipt at all, so this window is pure added latency on the happy path and
// must stay small.
const defaultHeldWait = 2 * time.Second
```

Extend the struct and constructor:

```go
type ClaudeSocketSender struct {
	sessionsDir string

	// receipts is rocket's own listening socket, used as the reply address so
	// the recipient can tell us a message was held. nil disables the wait: we
	// write and hope, which is the pre-receipt behavior.
	receipts *socketmsg.Receipts

	// heldWait overrides defaultHeldWait in tests.
	heldWait time.Duration
}

func NewClaudeSocketSender(sessionsDir string) *ClaudeSocketSender {
	return &ClaudeSocketSender{sessionsDir: sessionsDir, heldWait: defaultHeldWait}
}

// SetReceipts installs the reply channel. Called once during daemon wiring,
// before delivery starts.
func (s *ClaudeSocketSender) SetReceipts(r *socketmsg.Receipts) { s.receipts = r }
```

Replace `Send` with:

```go
func (s *ClaudeSocketSender) Send(sess store.Session, fromName, text string) error {
	cc, ok := s.lookup(sess)
	if !ok {
		return ErrNoSocket
	}

	// Reply address, msg id and waiter must all exist BEFORE the write: the
	// `held` receipt is emitted from the recipient's inbound handler and can
	// land before Send returns.
	//
	// The address has to sit in the recipient's own socket directory and end
	// in .sock or the receipt is silently not sent (protocol doc §7) — hence
	// deriving the directory from the recipient's path rather than picking one.
	var (
		from   string
		msgID  string
		wait   <-chan socketmsg.Message
		cancel = func() {}
	)
	if s.receipts != nil {
		addr, err := s.receipts.Addr(filepath.Dir(cc.MessagingSocketPath))
		if err != nil {
			// No reply channel: deliver blind rather than not at all.
			slog.Warn("queue: no receipt listener, socket delivery cannot detect a hold",
				"error", err)
		} else if id, err := socketmsg.NewMsgID(); err != nil {
			slog.Warn("queue: cannot mint msg id for receipt correlation", "error", err)
		} else {
			from, msgID = addr, id
			wait, cancel = s.receipts.Watch(id)
		}
	}
	defer cancel()

	if _, err := socketmsg.Send(cc.MessagingSocketPath, text, socketmsg.Options{
		From:     from,
		MsgID:    msgID,
		FromName: fromName,
		Priority: socketmsg.PriorityNext,
		// Pin the recipient's identity: if the pid was recycled between the
		// registry read and this write, the new owner of the socket drops the
		// message instead of showing a stranger's text (protocol doc §8).
		SessionID: cc.SessionID,
		// from-mode is deliberately never asserted — claiming "bypass" would
		// let our messages through a bypass-mode recipient's guard with
		// privileges we have no business claiming (protocol doc §5.2). We
		// take the hold and route around it instead.
	}); err != nil {
		return err
	}

	if wait == nil {
		return nil
	}
	return s.awaitReceipt(wait)
}

// awaitReceipt turns the recipient's verdict into an error, or nil.
//
// Silence is success: `accept` never produces a receipt (protocol doc §7), so
// the only thing this window can catch is a hold — which is emitted at once.
// `denied`/`expired` only ever follow a `held` we already reacted to.
func (s *ClaudeSocketSender) awaitReceipt(wait <-chan socketmsg.Message) error {
	window := s.heldWait
	if window <= 0 {
		window = defaultHeldWait
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case m := <-wait:
		switch m.Status {
		case "held", "denied", "expired":
			return fmt.Errorf("%w (status %s)", ErrHeld, m.Status)
		default:
			return nil
		}
	case <-timer.C:
		return nil
	}
}
```

Add `"fmt"` to the imports.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/queue/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue/socket.go internal/queue/socket_test.go
git commit -m "queue: считать придержанное сообщение недоставленным"
```

---

### Task 3: Queue-level fallback test and daemon wiring

**Files:**
- Modify: `internal/queue/queue_socket_test.go`
- Modify: `internal/queue/queue.go:550` (log line only)
- Modify: `internal/daemon/daemon.go:81`

**Interfaces:**
- Consumes: `ErrHeld`, `SetReceipts`, `socketmsg.NewReceipts` from Tasks 1–2.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test** (append to `queue_socket_test.go`)

```go
// Дыра, ради которой всё затевалось: получатель придержал сообщение, а очередь
// раньше считала его доставленным. Теперь ErrHeld обязан увести доставку в
// tmux, и сообщение всё-таки дойдёт.
func TestHeldMessageFallsBackToInject(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true, sendErr: ErrHeld}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{FromSession: "orch", ToSession: "recv", Body: "важное"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered via tmux after hold")
	if n := h.rt.callCount(); n != 1 {
		t.Errorf("Inject called %d times, want 1 — придержанное сообщение обязано уйти в tmux", n)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/queue/ -run TestHeldMessageFallsBackToInject -v`
Expected: PASS immediately — `attemptDelivery` already falls back on any `Send` error. This test is a regression pin for that contract, not new behavior; if it fails, the fallback wiring is broken and must be fixed before continuing.

- [ ] **Step 3: Make the fallback log name the hold**

In `internal/queue/queue.go`, in `attemptDelivery`, replace the fallback log with one that distinguishes a hold from a transport failure:

```go
			if errors.Is(sendErr, ErrHeld) {
				slog.Warn("queue: recipient held the message, falling back to tmux injection",
					"id", msg.ID, "to", msg.ToSession, "error", sendErr)
			} else {
				slog.Info("queue: socket delivery failed, falling back to tmux injection",
					"id", msg.ID, "to", msg.ToSession, "error", sendErr)
			}
```

- [ ] **Step 4: Wire the listener in the daemon**

In `internal/daemon/daemon.go`, replace the `q.SetSocketSender(...)` line with:

```go
	// Claude Code recipients get their messages over the cross-session UDS
	// inbox whenever one is reachable; every other agent keeps using tmux, and
	// so does this path whenever the socket is missing, refuses the write, or
	// the recipient holds the message for approval.
	receipts := socketmsg.NewReceipts("rocketd")
	defer receipts.Close()
	socketSender := queue.NewClaudeSocketSender(socketmsg.SessionsDir())
	socketSender.SetReceipts(receipts)
	q.SetSocketSender(socketSender)
```

- [ ] **Step 5: Build and run the whole suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/queue/ internal/daemon/daemon.go
git commit -m "daemon: слушать квитанции и уводить придержанные сообщения в tmux"
```

---

### Task 4: Documentation

**Files:**
- Modify: `docs/06-messaging.md`
- Modify: `docs/design/cc-socket-protocol.md` (§7 — record that rocket now implements the reply channel)

- [ ] **Step 1: Update `docs/06-messaging.md`**

In the socket-transport section, add: rocket listens on `<socket-dir>/rocketd-<pid>.sock` and advertises it as the message's `from`. A `held` receipt (a bypass-permissions recipient without `crossSessionInbound=accept`) is treated as non-delivery and the message is injected via tmux instead. Silence within a 2-second window means accepted — the protocol never acks an accepted message. Note the accepted trade-off: if the user later approves the held copy, the recipient sees the message twice. A visible duplicate beats a silent loss, and the protocol offers no way to withdraw a held message.

- [ ] **Step 2: Update `docs/design/cc-socket-protocol.md` §7**

Change the "Следствие для rocket" paragraph from a recommendation to a statement of what is implemented, pointing at `internal/socketmsg/receipts.go`.

- [ ] **Step 3: Commit**

```bash
git add docs/
git commit -m "docs: обратный канал квитанций и фолбэк на tmux при hold"
```

---

### Task 5: Per-session "held once → prefer tmux" memory

**Files:**
- Modify: `internal/queue/socket.go`
- Modify: `internal/queue/socket_test.go`

**Interfaces:**
- Produces: unexported `held map[string]bool` on `ClaudeSocketSender`, keyed by the recipient's Claude `sessionId`.

Rationale: a recipient that held one message will hold the next one too — its
`crossSessionInbound` policy does not change while the process lives. Without
this, every subsequent message to that worker pays the full 2-second wait
before falling back. The `sessionId` is a fresh UUID per `claude` process, so
the memory expires by construction when the session restarts; in-memory is
therefore the right lifetime, and a daemon restart only costs one more 2s
probe per session.

- [ ] **Step 1: Write the failing test**

```go
// Второе сообщение той же сессии не должно снова ждать 2 с: сессия уже
// показала, что придерживает peer-сообщения, и не изменит политику, пока жива.
func TestHeldSessionPrefersTmuxAfterwards(t *testing.T) {
	sockDir := shortTempDir(t)
	regDir := t.TempDir()
	path := heldSocket(t, sockDir, "996")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 996, SessionID: "sid-996", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	r := socketmsg.NewReceipts("rocket-test")
	defer r.Close()
	s := NewClaudeSocketSender(regDir)
	s.SetReceipts(r)
	s.heldWait = 3 * time.Second

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	if !s.Available(sess) {
		t.Fatal("сессия должна быть доступна до первого hold")
	}
	if err := s.Send(sess, "orch", "первое"); !errors.Is(err, ErrHeld) {
		t.Fatalf("Send err = %v, want ErrHeld", err)
	}
	if s.Available(sess) {
		t.Error("после hold сокет для этой сессии больше не предлагаем")
	}
}
```

- [ ] **Step 2: Run it, watch it fail** — `go test ./internal/queue/ -run TestHeldSessionPrefersTmux -v`

- [ ] **Step 3: Implement**

Add to the struct: `heldMu sync.Mutex` and `heldSessions map[string]bool`, initialised in the constructor. In `lookup`, after a match is resolved, treat a remembered session as no match:

```go
	if s.heldRecently(match.SessionID) {
		return socketmsg.Session{}, false
	}
```

and record it in `Send` when `awaitReceipt` returns `ErrHeld`.

- [ ] **Step 4: Run tests, then commit**

```bash
git add internal/queue/
git commit -m "queue: помнить сессии, которые придерживают peer-сообщения"
```

---

### Task 6: Sweep stale `rocketd-*.sock` files

**Files:**
- Modify: `internal/socketmsg/receipts.go`
- Modify: `internal/socketmsg/receipts_test.go`

A crashed daemon leaves its socket file behind; a long-lived `/tmp/cc-socks/`
would otherwise accumulate one per crash. On creating a listener in a
directory, remove every `<prefix>-*.sock` in it that does not answer a probe
(and is not the path we are about to bind).

- [ ] **Step 1: Test** — create `<dir>/rocket-test-1.sock` as a plain file, call `Addr(dir)`, assert the stale file is gone and a live socket bound by another `Receipts` in the same dir survives.

- [ ] **Step 2..4:** implement `sweepStale(dir, keep string)` called from `Addr` before `net.Listen`, run tests, commit.

---

## Verification before completion

- [ ] `go build ./... && go vet ./... && go test ./...` — all green, output pasted into the PR.
- [ ] `go test ./internal/queue/ ./internal/socketmsg/ -race` — no data races (the receipt listener is concurrent by construction).
- [ ] End-to-end manual check with the real CLI, using `cmd/ccsend`: start a session with `--dangerously-skip-permissions` and no `crossSessionInbound`, send to it with `CCSEND_REPLY_SOCKET` pointing into `/tmp/cc-socks/`, confirm a `held` receipt arrives on the listener.
