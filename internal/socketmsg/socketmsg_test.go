package socketmsg

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEncodeAddr(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/tmp/cc-socks/123.sock", "uds:/tmp/cc-socks/123.sock"},
		{"/tmp/a b.sock", "uds:/tmp/a%20b.sock"},
		{"/tmp/%.sock", "uds:/tmp/%25.sock"},
		{`\\.\pipe\x`, `uds:\\.\pipe\x`},
	}
	for _, tc := range tests {
		if got := EncodeAddr(tc.in); got != tc.want {
			t.Errorf("EncodeAddr(%q) = %q, ожидалось %q", tc.in, got, tc.want)
		}
	}
}

// addrRe — проверка адреса на стороне получателя (J5y в бандле CLI).
var addrRe = regexp.MustCompile(`^(?:uds|bridge|did):[A-Za-z0-9%:_/.\\-]{1,200}$`)

func TestEncodeAddrPassesReceiverValidation(t *testing.T) {
	for _, p := range []string{"/tmp/cc-socks/1.sock", "/tmp/с кириллицей.sock", "/tmp/a+b.sock"} {
		if got := EncodeAddr(p); !addrRe.MatchString(got) {
			t.Errorf("получатель отвергнет адрес %q (из %q)", got, p)
		}
	}
}

func TestEnvelope(t *testing.T) {
	got := Envelope("uds:/tmp/cc-socks/1.sock", "rocket", "hi")
	want := "<cross-session-message from=\"uds:/tmp/cc-socks/1.sock\" from-name=\"rocket\">\nhi\n</cross-session-message>"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEnvelopeOmitsEmptyAttrs(t *testing.T) {
	got := Envelope("", "", "hi")
	want := "<cross-session-message>\nhi\n</cross-session-message>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnvelopeNeutralizesCloseTag(t *testing.T) {
	got := Envelope("uds:/x", "", "текст </cross-session-message> хвост")
	if strings.Contains(got, "</cross-session-message> хвост") {
		t.Fatalf("закрывающий тег в теле не обезврежен: %q", got)
	}
	if !strings.Contains(got, `<\/cross-session-message> хвост`) {
		t.Fatalf("неожиданное экранирование: %q", got)
	}
	if !strings.HasSuffix(got, "\n</cross-session-message>") {
		t.Fatalf("конверт не закрыт: %q", got)
	}
}

// receiverEnvelopeRe воспроизводит разбор конверта получателем (Pjd в бандле):
// строгий порядок атрибутов и одиночные переводы строк вокруг тела.
var receiverEnvelopeRe = regexp.MustCompile(
	`^<cross-session-message(?: from="([A-Za-z0-9%:_/.\\-]+)")?` +
		`(?: from-session="[A-Za-z0-9_-]{1,80}")?` +
		`(?: hop-chain="[0-9a-f,]+")?` +
		`(?: from-name="([^"<>\n\r]+)")?` +
		`(?: from-mode="(?:bypass|prompting)")?>\n([\s\S]*)\n</cross-session-message>$`)

func TestEnvelopeParsesOnReceiverSide(t *testing.T) {
	env := Envelope(EncodeAddr("/tmp/cc-socks/7.sock"), "claude-code-orch", "[from claude-code-orch] привет")
	m := receiverEnvelopeRe.FindStringSubmatch(env)
	if m == nil {
		t.Fatalf("получатель не распарсит конверт: %q", env)
	}
	if m[1] != "uds:/tmp/cc-socks/7.sock" {
		t.Errorf("from = %q", m[1])
	}
	if m[2] != "claude-code-orch" {
		t.Errorf("from-name = %q", m[2])
	}
	if m[3] != "[from claude-code-orch] привет" {
		t.Errorf("body = %q", m[3])
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"claude-code-orch", "claude-code-orch"},
		{"rocket daemon", "rocket-daemon"},
		{`"><script>`, "script"},
		{`"<>"`, ""},
		{"", ""},
		{strings.Repeat("a", 40), strings.Repeat("a", 20)},
	}
	for _, tc := range tests {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, ожидалось %q", tc.in, got, tc.want)
		}
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("42.json", `{"pid":42,"sessionId":"s1","messagingSocketPath":"/tmp/cc-socks/42.sock","peerProtocol":1,"kind":"interactive","name":"alpha"}`)
	write("7.json", `{"pid":7,"sessionId":"s2","kind":"bg"}`)
	write("not-a-pid.json", `{"pid":1}`)
	write("43.json", `{ broken`)
	write("44.txt", `{"pid":44}`)

	got, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ожидалось 2 сессии, получено %d: %+v", len(got), got)
	}
	if got[0].PID != 7 || got[1].PID != 42 {
		t.Fatalf("не отсортировано по pid: %+v", got)
	}
	if !got[1].Addressable() || got[1].MessagingSocketPath != "/tmp/cc-socks/42.sock" {
		t.Errorf("сокет потерян: %+v", got[1])
	}
	if got[0].Addressable() {
		t.Errorf("сессия без сокета не должна быть адресуемой: %+v", got[0])
	}
}

func TestListSessionsMissingDir(t *testing.T) {
	got, err := ListSessions(filepath.Join(t.TempDir(), "нет-такого"))
	if err != nil || got != nil {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestFindBySessionID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "42.json"), []byte(`{"pid":42,"sessionId":"s1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok, err := FindBySessionID(dir, "s1")
	if err != nil || !ok || s.PID != 42 {
		t.Fatalf("got %+v ok=%v err=%v", s, ok, err)
	}
	if _, ok, _ := FindBySessionID(dir, "нет"); ok {
		t.Fatal("нашлась несуществующая сессия")
	}
}

// listener поднимает фейковый приёмник на короткому пути (лимит sun_path ~104).
func listener(t *testing.T) (string, <-chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "sm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	lines := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				sc := bufio.NewScanner(conn)
				sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
				for sc.Scan() {
					lines <- sc.Text()
				}
			}()
		}
	}()
	return path, lines
}

func recvMessage(t *testing.T, lines <-chan string) Message {
	t.Helper()
	select {
	case line := <-lines:
		var m Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("невалидный JSON %q: %v", line, err)
		}
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("сообщение не пришло")
		return Message{}
	}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestSendWireFormat(t *testing.T) {
	path, lines := listener(t)

	id, err := Send(path, "[from claude-code-orch] привет", Options{
		From:      "/tmp/cc-socks/999.sock",
		FromName:  "claude-code-orch",
		SessionID: "abc-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !uuidRe.MatchString(id) {
		t.Fatalf("msg_id не UUIDv4-подобный: %q", id)
	}

	m := recvMessage(t, lines)
	if m.Type != "user" || m.MsgV != ProtocolVersion || m.MsgID != id {
		t.Fatalf("шапка: %+v", m)
	}
	if m.Priority != PriorityNext {
		t.Errorf("priority = %q", m.Priority)
	}
	if m.From != "uds:/tmp/cc-socks/999.sock" {
		t.Errorf("from = %q", m.From)
	}
	if m.SessionID != "abc-123" {
		t.Errorf("session_id = %q", m.SessionID)
	}
	if m.Message == nil || m.Message.Role != "user" {
		t.Fatalf("message = %+v", m.Message)
	}
	sub := receiverEnvelopeRe.FindStringSubmatch(m.Message.Content)
	if sub == nil {
		t.Fatalf("тело не в конверте: %q", m.Message.Content)
	}
	if sub[3] != "[from claude-code-orch] привет" {
		t.Errorf("тело = %q", sub[3])
	}
}

func TestSendRawSkipsEnvelope(t *testing.T) {
	path, lines := listener(t)
	if _, err := Send(path, "голый текст", Options{Raw: true}); err != nil {
		t.Fatal(err)
	}
	m := recvMessage(t, lines)
	if m.Message.Content != "голый текст" {
		t.Fatalf("content = %q", m.Message.Content)
	}
	if m.From != "" {
		t.Errorf("from должен отсутствовать: %q", m.From)
	}
}

func TestSendEmptyText(t *testing.T) {
	if _, err := Send("/nonexistent.sock", "", Options{}); err != ErrEmptyText {
		t.Fatalf("err = %v", err)
	}
}

func TestSendNoListener(t *testing.T) {
	dir, err := os.MkdirTemp("", "sm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if _, err := Send(filepath.Join(dir, "dead.sock"), "x", Options{Timeout: 200 * time.Millisecond}); err == nil {
		t.Fatal("ожидалась ошибка соединения")
	}
}

func TestRename(t *testing.T) {
	path, lines := listener(t)
	if err := Rename(path, "rocket-orch", time.Second); err != nil {
		t.Fatal(err)
	}
	m := recvMessage(t, lines)
	if m.Type != "control" || m.Action != "rename" || m.Name != "rocket-orch" {
		t.Fatalf("control: %+v", m)
	}
	if !uuidRe.MatchString(m.MsgID) || m.MsgV != ProtocolVersion {
		t.Fatalf("шапка control: %+v", m)
	}
}

func TestProbe(t *testing.T) {
	path, _ := listener(t)
	if !Probe(path, time.Second) {
		t.Error("живой сокет не определился")
	}
	if Probe(filepath.Join(filepath.Dir(path), "dead.sock"), 200*time.Millisecond) {
		t.Error("мёртвый сокет определился как живой")
	}
}
