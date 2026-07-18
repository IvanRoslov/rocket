package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/version"
)

func testDeps(t *testing.T, shutdown func()) Deps {
	t.Helper()
	if shutdown == nil {
		shutdown = func() {}
	}
	return Deps{
		Cfg:       &config.Config{},
		Shutdown:  shutdown,
		StartedAt: time.Now().Add(-42 * time.Second),
	}
}

func TestHealthEndpoint(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Uptime  string `json:"uptime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.Version != version.Version {
		t.Errorf("version = %q, want %q", body.Version, version.Version)
	}
	if body.Uptime == "" {
		t.Errorf("uptime is empty")
	}
	if _, err := time.ParseDuration(body.Uptime); err != nil {
		t.Errorf("uptime %q not a valid duration: %v", body.Uptime, err)
	}
}

func TestUnknownPathReturns404WithErrorShape(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatalf("GET /nope: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Error.Code)
	}
	if body.Error.Message == "" {
		t.Errorf("message is empty")
	}
}

func TestShutdownEndpoint(t *testing.T) {
	called := make(chan struct{})
	d := testDeps(t, func() { close(called) })
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/shutdown", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/shutdown: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "shutting_down" {
		t.Errorf("status = %q, want shutting_down", body.Status)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown callback was not invoked")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestServeUnixSocket(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	cfg := &config.Config{Home: home, Port: freePort(t)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := Deps{Cfg: cfg, Shutdown: func() {}, StartedAt: time.Now()}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, d)
	}()

	sockPath := cfg.SocketPath()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if fi, err := os.Stat(sockPath); err == nil {
			if fi.Mode().Perm() != 0600 {
				t.Fatalf("socket mode = %v, want 0600", fi.Mode().Perm())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for socket file")
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", sockPath)
			},
		},
	}

	resp, err := client.Get("http://unix/v1/health")
	if err != nil {
		t.Fatalf("GET over unix socket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Also verify TCP listener works.
	tcpResp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(cfg.Port) + "/v1/health")
	if err != nil {
		t.Fatalf("GET over tcp: %v", err)
	}
	defer tcpResp.Body.Close()
	if tcpResp.StatusCode != http.StatusOK {
		t.Fatalf("tcp status = %d, want 200", tcpResp.StatusCode)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("socket file still exists after shutdown: err=%v", err)
	}
}
