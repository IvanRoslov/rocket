package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

// TestBuildSendBodyUsageViolations tests body extraction/validation.
//
// Argument-shape (XOR) validation now lives in RunE (see
// TestSendUsageErrors below); buildSendBody only reads the body from
// whichever source is given and checks it is non-empty.
func TestBuildSendBodyUsageViolations(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("file content"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name     string
		args     []string
		filePath string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "no args",
			args:     []string{},
			filePath: "",
			wantErr:  true,
			errMsg:   "body must not be empty",
		},
		{
			name:     "only session",
			args:     []string{"session-id"},
			filePath: "",
			wantErr:  true,
			errMsg:   "body must not be empty",
		},
		{
			name:     "three args",
			args:     []string{"session-id", "arg2", "arg3"},
			filePath: "",
			wantErr:  true,
			errMsg:   "body must not be empty",
		},
		{
			name:     "file not found",
			args:     []string{"session-id"},
			filePath: filepath.Join(tmpDir, "nonexistent.txt"),
			wantErr:  true,
			errMsg:   "no such file or directory",
		},
		{
			name:     "empty body text",
			args:     []string{"session-id", ""},
			filePath: "",
			wantErr:  true,
			errMsg:   "body must not be empty",
		},
		{
			name:     "valid with text",
			args:     []string{"session-id", "hello world"},
			filePath: "",
			wantErr:  false,
		},
		{
			name:     "valid with file",
			args:     []string{"session-id"},
			filePath: testFile,
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildSendBody(&cobra.Command{}, tc.args, tc.filePath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errMsg != "" && !containsSubstring(err.Error(), tc.errMsg) {
					t.Errorf("error message: got %q, want substring %q", err.Error(), tc.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if body == "" {
					t.Errorf("body should not be empty")
				}
			}
		})
	}
}

// TestBuildSendBodyContent tests that body content is read correctly.
func TestBuildSendBodyContent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "file content with\nmultiple lines\nand stuff"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name     string
		args     []string
		filePath string
		want     string
	}{
		{
			name:     "text from args",
			args:     []string{"session-id", "hello world"},
			filePath: "",
			want:     "hello world",
		},
		{
			name:     "text with quotes",
			args:     []string{"session-id", "hello \"world\""},
			filePath: "",
			want:     "hello \"world\"",
		},
		{
			name:     "multiline text",
			args:     []string{"session-id", "line1\nline2\nline3"},
			filePath: "",
			want:     "line1\nline2\nline3",
		},
		{
			name:     "content from file",
			args:     []string{"session-id"},
			filePath: testFile,
			want:     content,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildSendBody(&cobra.Command{}, tc.args, tc.filePath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if body != tc.want {
				t.Errorf("got %q, want %q", body, tc.want)
			}
		})
	}
}

// containsSubstring checks if haystack contains needle as a substring.
func containsSubstring(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestSendUsageErrors verifies that every way of violating `send`'s
// argument shape (missing body, too few/many args, or both a positional
// body and --file) is reported as a usage error (exit code 3), and — the
// finding this guards against — that `rocket send <session>` with neither
// text nor --file is included (previously this exited 1).
func TestSendUsageErrors(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("file content"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"no text, no file", []string{"my-session"}},
		{"too many args", []string{"my-session", "text", "extra"}},
		{"both text and --file", []string{"my-session", "text", "--file", testFile}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newSendCmd()
			cmd.SilenceUsage = true
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if exitCode(err) != 3 {
				t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
			}
		})
	}
}

// newUnixSocketTestServer starts an http.Server listening on a fresh unix
// socket at a short path (avoiding sun_path length limits that t.TempDir()
// can exceed) and returns a *client.Client bound to it plus a cleanup func.
func newUnixSocketTestServer(t *testing.T, handler http.Handler) *client.Client {
	t.Helper()

	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("rkt-send-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(sockPath) })

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sockPath, err)
	}

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	return client.New(sockPath)
}

// TestWaitForMessageDeliveringThenDelivered scripts a
// queued -> delivering -> delivered sequence and asserts waitForMessage
// returns nil once "delivered" is reached, without erroring on the
// intermediate "delivering" status (the bug this guards against: the old
// switch had no case for "delivering" and fell into `default`, which
// errored).
func TestWaitForMessageDeliveringThenDelivered(t *testing.T) {
	restore := setPollIntervalForTest(t, 10*time.Millisecond)
	defer restore()

	statuses := []string{"queued", "delivering", "delivered"}
	var seq int32

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/42", func(w http.ResponseWriter, r *http.Request) {
		i := atomic.AddInt32(&seq, 1) - 1
		status := statuses[len(statuses)-1]
		if int(i) < len(statuses) {
			status = statuses[i]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgResp{ID: 42, Status: status})
	})

	c := newUnixSocketTestServer(t, mux)

	cmd := &cobra.Command{}
	if err := waitForMessage(cmd, c, 42); err != nil {
		t.Fatalf("waitForMessage: unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&seq); got < int32(len(statuses)) {
		t.Fatalf("server only polled %d times, expected at least %d (delivering must not short-circuit the loop)", got, len(statuses))
	}
}

// TestWaitForMessageFailed asserts a "failed" status is reported as an
// error, and that the failure reason returned by the server is printed in
// the CLI's stderr output.
func TestWaitForMessageFailed(t *testing.T) {
	restore := setPollIntervalForTest(t, 10*time.Millisecond)
	defer restore()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/7", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgResp{ID: 7, Status: "failed", Reason: "recipient gone"})
	})

	c := newUnixSocketTestServer(t, mux)

	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := waitForMessage(cmd, c, 7)
	if err == nil {
		t.Fatal("expected error for failed status, got nil")
	}

	if !strings.Contains(errBuf.String(), "recipient gone") {
		t.Errorf("stderr output = %q, want it to contain the failure reason %q", errBuf.String(), "recipient gone")
	}
}

// setPollIntervalForTest overrides the package-level pollInterval for the
// duration of a test and returns a func to restore it.
func setPollIntervalForTest(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := pollInterval
	pollInterval = d
	return func() { pollInterval = old }
}

// TestBuildSendBodyFromStdin covers `rocket send <session> --file -`: the
// body is piped in, so markdown reaches the recipient with its backticks
// intact instead of being run by the shell.
func TestBuildSendBodyFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("piped `body`"))

	got, err := buildSendBody(cmd, []string{"session-id"}, stdinMarker)
	if err != nil {
		t.Fatalf("buildSendBody: %v", err)
	}
	if got != "piped `body`" {
		t.Errorf("got %q, want %q", got, "piped `body`")
	}
}

// TestBuildSendBodyEmptyStdin keeps the non-empty guarantee: an empty pipe
// is a rejected body, not a silently-sent blank message.
func TestBuildSendBodyEmptyStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))

	if _, err := buildSendBody(cmd, []string{"session-id"}, stdinMarker); err == nil {
		t.Fatal("expected an error for an empty piped body, got nil")
	}
}
