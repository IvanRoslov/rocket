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

// shortDir — каталог с заведомо коротким путём: t.TempDir() на macOS уводит в
// /var/folders/… и выходит за 103 байта sun_path, после чего bind падает.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "rr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
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
		t.Fatalf("addr = %q, want <dir>/*.sock в %s", addr, dir)
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

// Close снимает сокет с диска: мёртвый файл сокета остаётся жить и мешает
// следующему bind по тому же пути.
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

// Сокет упавшего демона остаётся файлом в каталоге. Новый слушатель обязан
// подмести такие огрызки — но не трогать чужой живой сокет.
func TestReceiptsSweepsStaleSockets(t *testing.T) {
	dir := shortDir(t)

	stale := filepath.Join(dir, "rocket-test-999999.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	live := NewReceipts("rocket-test")
	defer live.Close()
	livePath, err := live.Addr(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Чужой сокет с другим префиксом трогать нельзя вовсе.
	foreign := filepath.Join(dir, "12345.sock")
	if err := os.WriteFile(foreign, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	sweeper := NewReceipts("rocket-test")
	defer sweeper.Close()
	// Другой каталог у того же процесса не поможет: подметание идёт по
	// каталогу, поэтому вызываем sweepStale напрямую с чужим "keep".
	sweeper.sweepStale(dir, filepath.Join(dir, "rocket-test-0.sock"))

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("мёртвый сокет %s не подмели (err=%v)", stale, err)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("живой сокет %s снесли: %v", livePath, err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("чужой сокет %s снесли: %v", foreign, err)
	}
}
