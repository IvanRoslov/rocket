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
// peer_message_status о судьбе придержанных сообщений (протокол §7).
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

// NewReceipts создаёт обратный канал. prefix — начало имени файла сокета; к
// нему добавляется pid, чтобы два демона не дрались за один путь.
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
	r.sweepStale(dir, path)
	// Наш собственный сокет от прошлого запуска с тем же pid тоже мешает bind.
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
		return p, nil
	}
	r.listeners[dir] = ln
	r.paths[dir] = path
	r.mu.Unlock()

	go r.accept(ln)
	return path, nil
}

// sweepStale удаляет в каталоге наши собственные сокеты, оставшиеся от упавших
// демонов. Живой сокет отвечает на connect, мёртвый — нет; чужие файлы (без
// нашего префикса) не трогаем вовсе, keep — путь, который мы сейчас займём.
func (r *Receipts) sweepStale(dir, keep string) {
	matches, err := filepath.Glob(filepath.Join(dir, r.prefix+"-*.sock"))
	if err != nil {
		return
	}
	for _, p := range matches {
		if p == keep || Probe(p, 0) {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Debug("socketmsg: не удалось убрать мёртвый сокет квитанций",
				"path", p, "error", err)
		}
	}
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
		// Ожидающего нет: квитанция опоздала. denied/expired приходят много
		// позже окна ожидания, когда доставка уже ушла в tmux, — реагировать
		// на них уже нечем, но в журнале это видеть полезно.
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
