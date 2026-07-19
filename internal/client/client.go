// Package client implements the HTTP client rocket's CLI uses to talk to
// rocketd over its unix socket, including autostart of the daemon when it
// isn't running.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
)

// ErrDaemonUnavailable is returned by Connect when the daemon is not
// reachable and either autostart was disabled or the autostart attempt
// timed out. internal/cli maps this sentinel to exit code 2 without
// importing this package's error type directly (avoiding an import cycle
// since client must not import cli).
var ErrDaemonUnavailable = errors.New("daemon unavailable")

// APIError represents the {"error":{"code":"...","message":"..."}} envelope
// returned by the API on non-2xx responses.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Client is an HTTP client bound to rocketd's unix socket.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client that dials the unix socket at socketPath.
//
// The underlying http.Client deliberately has no global Timeout: requests
// instead get a per-request deadline via context (see requestTimeout),
// because Manager.Spawn/Restore run `git fetch` + `worktree add`
// synchronously and can legitimately take much longer than a typical
// request's 10s budget — a global Timeout would make the CLI report
// failure (context deadline exceeded) even though the daemon's spawn
// eventually succeeds.
func New(socketPath string) *Client {
	return &Client{
		baseURL: "http://rocket",
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// Get performs a GET request to path, decoding the JSON response into out.
func (c *Client) Get(path string, in, out any) error {
	return c.do(http.MethodGet, path, in, out)
}

// Post performs a POST request to path, decoding the JSON response into out.
func (c *Client) Post(path string, in, out any) error {
	return c.do(http.MethodPost, path, in, out)
}

// Patch performs a PATCH request to path, decoding the JSON response into out.
func (c *Client) Patch(path string, in, out any) error {
	return c.do(http.MethodPatch, path, in, out)
}

// Put performs a PUT request to path, decoding the JSON response into out.
func (c *Client) Put(path string, in, out any) error {
	return c.do(http.MethodPut, path, in, out)
}

// Delete performs a DELETE request to path, decoding the JSON response into out.
func (c *Client) Delete(path string, in, out any) error {
	return c.do(http.MethodDelete, path, in, out)
}

// defaultRequestTimeout bounds most requests: they're quick reads/writes
// against the daemon's in-memory state and local store.
const defaultRequestTimeout = 10 * time.Second

// spawnRequestTimeout bounds requests whose handler synchronously runs
// `git fetch` + `worktree add` (session spawn and restore), which can
// legitimately exceed defaultRequestTimeout on a slow network or a large
// repo.
//
// TODO(phase2): replace this path-based heuristic with per-call timeout
// options on Client once callers need finer control than "default" vs
// "spawn-like".
const spawnRequestTimeout = 5 * time.Minute

// requestTimeout returns the timeout to apply to a request, based on its
// method and path. See defaultRequestTimeout and spawnRequestTimeout.
func requestTimeout(method, path string) time.Duration {
	if method != http.MethodPost {
		return defaultRequestTimeout
	}
	p := path
	if i := strings.IndexAny(p, "?"); i >= 0 {
		p = p[:i]
	}
	if p == "/v1/sessions" || strings.HasSuffix(p, "/restore") {
		return spawnRequestTimeout
	}
	return defaultRequestTimeout
}

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(method, path))
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sid := os.Getenv("ROCKET_SESSION_ID"); sid != "" {
		req.Header.Set("X-Rocket-Session", sid)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp.StatusCode, respBody)
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func parseAPIError(status int, body []byte) error {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code == "" {
		return &APIError{
			Code:    fmt.Sprintf("http_%d", status),
			Message: string(body),
		}
	}
	return &APIError{Code: envelope.Error.Code, Message: envelope.Error.Message}
}

// healthCheckTimeout bounds a single health-probe request.
const healthCheckTimeout = 500 * time.Millisecond

// autostartPollInterval and autostartTimeout govern how long Connect waits
// for a freshly-spawned daemon to become reachable.
const (
	autostartPollInterval = 100 * time.Millisecond
	autostartTimeout      = 5 * time.Second
)

// staleDaemonRetryWindow and staleDaemonPollInterval bound how long Connect
// polls health when the socket is unreachable but the pid file names a
// still-alive process. That combination is the profile of a daemon that is
// mid graceful-shutdown (rocketd's shutdown sweep plus socket/pid-file
// cleanup can take several seconds — see internal/daemon.Run and
// internal/daemon.WaitForExit), not a crashed one. Racing straight to
// spawnDaemon in that window just spawns a second daemon that loses the
// pid-file claim to the still-alive old one and exits immediately,
// surfacing as "daemon unavailable" (a recurring papercut when `daemon
// start` runs right after `daemon stop`, e.g. via `make restart`).
const (
	staleDaemonRetryWindow  = 10 * time.Second
	staleDaemonPollInterval = 200 * time.Millisecond
)

// Connect returns a Client bound to cfg's socket, verifying the daemon is
// reachable via a health check first. If the daemon is not reachable:
//   - autostart == false: returns ErrDaemonUnavailable immediately.
//   - autostart == true: if the pid file names a still-alive process (the
//     daemon is likely mid-shutdown), briefly retries the health check
//     instead of immediately racing it to spawn a competing daemon; once
//     that process is gone or staleDaemonRetryWindow elapses, spawns
//     `rocket daemon run` as a detached background process and polls
//     health until it responds or autostartTimeout elapses, returning
//     ErrDaemonUnavailable on timeout.
//
// A healthy daemon is always detected immediately by the first health
// check above — none of the stale-daemon tolerance below affects that
// fast path.
func Connect(cfg *config.Config, autostart bool) (*Client, error) {
	c := New(cfg.SocketPath())
	if healthy(c) {
		return c, nil
	}
	if !autostart {
		return nil, ErrDaemonUnavailable
	}

	if pid, ok := readAlivePid(cfg); ok {
		if waitForHealthyOrDead(c, pid, staleDaemonRetryWindow) {
			return c, nil
		}
	}

	if err := spawnDaemon(cfg.SocketOverride); err != nil {
		return nil, fmt.Errorf("%w: spawn daemon: %v", ErrDaemonUnavailable, err)
	}

	deadline := time.Now().Add(autostartTimeout)
	for time.Now().Before(deadline) {
		if healthy(c) {
			return c, nil
		}
		time.Sleep(autostartPollInterval)
	}
	return nil, ErrDaemonUnavailable
}

// readAlivePid reads cfg's pid file and reports whether it names a
// currently-alive process. ok is false if the pid file is absent,
// unparsable, or names a process that is no longer alive.
func readAlivePid(cfg *config.Config) (pid int, ok bool) {
	data, err := os.ReadFile(cfg.PidPath())
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(string(data))
	if err != nil {
		return 0, false
	}
	return pid, pidAlive(pid)
}

// pidAlive reports whether pid refers to a live process, using signal 0
// (an existence/permission check that delivers no actual signal) — the
// same semantics internal/daemon uses to detect a stale pid file.
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if err == syscall.ESRCH {
		return false
	}
	// EPERM (or anything else): the process exists but we can't signal it,
	// treat as alive to be conservative.
	return true
}

// waitForHealthyOrDead polls c's health endpoint until it responds healthy
// (returns true), or pid is no longer alive (returns false, so the caller
// can move on to spawning a replacement without waiting out the rest of
// timeout), or timeout elapses (returns false).
func waitForHealthyOrDead(c *Client, pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthy(c) {
			return true
		}
		if !pidAlive(pid) {
			return false
		}
		time.Sleep(staleDaemonPollInterval)
	}
	return false
}

func healthy(c *Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// spawnDaemon launches `rocket daemon run` as a detached background
// process. If socketOverride is non-empty, it is passed through as
// --socket so the spawned daemon binds to the same socket path the parent
// process resolved (the child does not otherwise see the parent's --socket
// flag).
func spawnDaemon(socketOverride string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	args := []string{"daemon", "run"}
	if socketOverride != "" {
		args = append(args, "--socket", socketOverride)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Stdin = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
