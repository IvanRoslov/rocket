package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
)

// A socket a live daemon is serving must never be removed or stolen: the
// loser has to fail with ErrSocketInUse and leave the leader's socket
// intact and still accepting connections.
func TestListenUnixSingletonRefusesLiveSocket(t *testing.T) {
	sock := filepath.Join(shortDir(t), "rocket.sock")

	leader, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("leader listen: %v", err)
	}
	defer leader.Close()

	ln, err := listenUnixSingleton(sock)
	if err == nil {
		ln.Close()
		t.Fatal("listenUnixSingleton succeeded against a live socket, want ErrSocketInUse")
	}
	if !errors.Is(err, ErrSocketInUse) {
		t.Fatalf("err = %v, want ErrSocketInUse", err)
	}

	if _, statErr := os.Stat(sock); statErr != nil {
		t.Fatalf("live socket was removed: %v", statErr)
	}
	conn, dialErr := net.Dial("unix", sock)
	if dialErr != nil {
		t.Fatalf("leader socket no longer accepts connections: %v", dialErr)
	}
	conn.Close()
}

// A socket file left behind by a crashed daemon has no listener: the new
// daemon removes it and binds.
func TestListenUnixSingletonReplacesStaleSocket(t *testing.T) {
	sock := filepath.Join(shortDir(t), "rocket.sock")

	dead, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// Close without unlinking, exactly like an uncleanly killed daemon.
	if ul, ok := dead.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	dead.Close()

	ln, err := listenUnixSingleton(sock)
	if err != nil {
		t.Fatalf("listenUnixSingleton over a stale socket: %v", err)
	}
	defer ln.Close()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial fresh socket: %v", err)
	}
	conn.Close()
}

// No socket file at all is the ordinary first-start case.
func TestListenUnixSingletonBindsFreshPath(t *testing.T) {
	sock := filepath.Join(shortDir(t), "rocket.sock")

	ln, err := listenUnixSingleton(sock)
	if err != nil {
		t.Fatalf("listenUnixSingleton: %v", err)
	}
	defer ln.Close()

	if fi, err := os.Stat(sock); err != nil {
		t.Fatalf("stat socket: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Fatalf("socket mode = %v, want 0600", fi.Mode().Perm())
	}
}

// End to end: a second Serve against a home whose daemon is already
// serving must fail and leave the running daemon's socket working.
func TestServeRefusesWhenSocketIsLive(t *testing.T) {
	home := shortDir(t)
	cfg := &config.Config{Home: home, Port: freePort(t)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leaderErr := make(chan error, 1)
	go func() {
		leaderErr <- Serve(ctx, Deps{Cfg: cfg, Shutdown: func() {}, StartedAt: time.Now()})
	}()

	sock := cfg.SocketPath()
	waitForSocket(t, sock)

	loserCfg := &config.Config{Home: home, Port: freePort(t)}
	err := Serve(context.Background(), Deps{Cfg: loserCfg, Shutdown: func() {}, StartedAt: time.Now()})
	if !errors.Is(err, ErrSocketInUse) {
		t.Fatalf("loser Serve err = %v, want ErrSocketInUse", err)
	}

	// The leader is still serving on the very same socket.
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://unix/v1/health")
	if err != nil {
		t.Fatalf("leader health after losing start: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("leader health status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := <-leaderErr; err != nil {
		t.Fatalf("leader Serve returned %v", err)
	}
}

// A losing start that fails on the TCP bind must not take the socket down
// with it — neither its own (it never got one) nor the leader's.
func TestServeTCPBindFailureLeavesLiveSocketIntact(t *testing.T) {
	home := shortDir(t)
	cfg := &config.Config{Home: home, Port: freePort(t)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leaderErr := make(chan error, 1)
	go func() {
		leaderErr <- Serve(ctx, Deps{Cfg: cfg, Shutdown: func() {}, StartedAt: time.Now()})
	}()

	sock := cfg.SocketPath()
	waitForSocket(t, sock)

	// Same home (same socket) AND the leader's port: both claims collide.
	err := Serve(context.Background(), Deps{Cfg: &config.Config{Home: home, Port: cfg.Port}, Shutdown: func() {}, StartedAt: time.Now()})
	if err == nil {
		t.Fatal("second Serve succeeded, want an error")
	}
	if _, statErr := os.Stat(sock); statErr != nil {
		t.Fatalf("leader socket removed by the losing start: %v", statErr)
	}
	conn, dialErr := net.Dial("unix", sock)
	if dialErr != nil {
		t.Fatalf("leader socket no longer accepts connections: %v", dialErr)
	}
	conn.Close()

	cancel()
	if err := <-leaderErr; err != nil {
		t.Fatalf("leader Serve returned %v", err)
	}
}

// The incident in one test: a live daemon plus N concurrent starts. Every
// loser must fail, and the leader's socket must still be *serving* — not
// merely present on disk, which is exactly what the old code left behind
// right up to the moment it unlinked it.
func TestServeConcurrentStartsNeverKillTheLiveSocket(t *testing.T) {
	home := shortDir(t)
	cfg := &config.Config{Home: home, Port: freePort(t)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leaderErr := make(chan error, 1)
	go func() {
		leaderErr <- Serve(ctx, Deps{Cfg: cfg, Shutdown: func() {}, StartedAt: time.Now()})
	}()

	sock := cfg.SocketPath()
	waitForSocket(t, sock)

	const losers = 5
	var wg sync.WaitGroup
	errs := make([]error, losers)
	for i := 0; i < losers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct ports: the socket alone must decide the singleton,
			// with no help from a TCP collision.
			loser := &config.Config{Home: home, Port: freePort(t)}
			errs[i] = Serve(context.Background(), Deps{Cfg: loser, Shutdown: func() {}, StartedAt: time.Now()})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, ErrSocketInUse) {
			t.Errorf("loser %d: err = %v, want ErrSocketInUse", i, err)
		}
	}

	// The leader is still answering on the same path.
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://unix/v1/health")
	if err != nil {
		t.Fatalf("leader health after %d losing starts: %v", losers, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("leader health status = %d, want 200", resp.StatusCode)
	}

	cancel()
	if err := <-leaderErr; err != nil {
		t.Fatalf("leader Serve returned %v", err)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if conn, err := net.Dial("unix", path); err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for a listener on %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// shortDir returns a fresh temp dir with a short path: unix socket paths
// are capped at ~104 bytes on macOS and t.TempDir() embeds the test name,
// which blows past the limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rkt")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
