package daemon

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestRedirectFDToFile: whatever the runtime writes to the redirected
// descriptor — a panic, the goroutine dump of `kill -QUIT` — lands in the
// file instead of /dev/null, appended after a header naming the process.
func TestRedirectFDToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "rocketd.stderr.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A throwaway descriptor stands in for fd 2, so the test's own stderr is
	// left alone.
	stand, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatal(err)
	}
	defer stand.Close()

	if err := redirectFD(int(stand.Fd()), path); err != nil {
		t.Fatalf("redirectFD: %v", err)
	}
	if _, err := stand.WriteString("goroutine 1 [running]:\n"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "earlier run\n") {
		t.Errorf("previous content must be kept (append), got %q", s)
	}
	if !strings.Contains(s, "rocketd stderr") || !strings.Contains(s, "pid") {
		t.Errorf("expected a header line naming the process, got %q", s)
	}
	if !strings.HasSuffix(s, "goroutine 1 [running]:\n") {
		t.Errorf("writes to the descriptor must land in the file, got %q", s)
	}
}

// TestCaptureStderrKeepsQuitDump is the whole point end to end: a process
// detached with stderr on /dev/null, like the autostarted daemon, gets
// `kill -QUIT` — and the runtime's goroutine dump is in the file afterwards.
func TestCaptureStderrKeepsQuitDump(t *testing.T) {
	if logPath := os.Getenv("ROCKET_TEST_CAPTURE_HELPER"); logPath != "" {
		if err := captureStderr(logPath); err != nil {
			os.Exit(3)
		}
		fmt.Println("ready")
		select {} // wait for SIGQUIT
	}

	logPath := filepath.Join(t.TempDir(), "logs", "rocketd.log")
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestCaptureStderrKeepsQuitDump$")
	cmd.Env = append(os.Environ(), "ROCKET_TEST_CAPTURE_HELPER="+logPath)
	cmd.Stderr = devNull
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		cmd.Process.Kill()
		t.Fatalf("helper did not start: %q", line)
	}
	if err := cmd.Process.Signal(syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // exits non-zero after the dump, as SIGQUIT does

	got, err := os.ReadFile(stderrLogPath(logPath))
	if err != nil {
		t.Fatalf("stderr log not written: %v", err)
	}
	if !strings.Contains(string(got), "SIGQUIT") || !strings.Contains(string(got), "goroutine ") {
		t.Fatalf("expected the goroutine dump in the file, got:\n%s", got)
	}
}
