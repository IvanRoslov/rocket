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
			Timeout: 10 * time.Second,
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

// Delete performs a DELETE request to path, decoding the JSON response into out.
func (c *Client) Delete(path string, in, out any) error {
	return c.do(http.MethodDelete, path, in, out)
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

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

// Connect returns a Client bound to cfg's socket, verifying the daemon is
// reachable via a health check first. If the daemon is not reachable:
//   - autostart == false: returns ErrDaemonUnavailable immediately.
//   - autostart == true: spawns `rocket daemon run` as a detached background
//     process and polls health until it responds or autostartTimeout elapses,
//     returning ErrDaemonUnavailable on timeout.
func Connect(cfg *config.Config, autostart bool) (*Client, error) {
	c := New(cfg.SocketPath())
	if healthy(c) {
		return c, nil
	}
	if !autostart {
		return nil, ErrDaemonUnavailable
	}

	if err := spawnDaemon(); err != nil {
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

func spawnDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	cmd := exec.Command(exe, "daemon", "run")
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Stdin = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
