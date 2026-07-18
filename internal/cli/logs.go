package cli

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// logsPollInterval is how often `rocket logs --follow` polls the log file
// for growth.
const logsPollInterval = 1 * time.Second

func newLogsCmd() *cobra.Command {
	var follow bool
	var n int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Показать логи rocketd",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket logs [--follow] [-n N]"}
			}

			// This command deliberately does not autostart (or even
			// contact) the daemon: it reads the log file straight off
			// disk, so it works even when rocketd isn't running.
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			logPath := cfg.LogPath()
			w := cmd.OutOrStdout()

			size, err := tailFile(logPath, n, w)
			if err != nil {
				return err
			}

			if !follow {
				return nil
			}
			return followFile(cmd.Context(), logPath, size, w)
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "продолжать выводить новые строки")
	cmd.Flags().IntVarP(&n, "lines", "n", 100, "число последних строк")
	return cmd
}

// tailFile prints the last n lines of the file at path to w, and returns
// the file's size at the time it was read (used as the starting offset for
// followFile). A missing file is treated as empty, not an error.
func tailFile(path string, n int, w io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}

	lines, err := lastNLines(f, n)
	if err != nil {
		return 0, err
	}
	for _, line := range lines {
		_, _ = io.WriteString(w, line+"\n")
	}
	return fi.Size(), nil
}

// lastNLines returns the last n lines of r. n <= 0 means "no lines".
func lastNLines(r io.Reader, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	buf := make([]string, 0, n)
	for scanner.Scan() {
		buf = append(buf, scanner.Text())
		if len(buf) > n {
			buf = buf[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buf, nil
}

// followFile polls the file at path for growth every logsPollInterval,
// printing newly-appended bytes to w, starting from offset. If the file
// shrinks (e.g. rotation replaced it with a fresh, smaller file), it
// reopens from the start. It returns on SIGINT/SIGTERM or ctx cancellation
// (a clean exit), or on a read error.
func followFile(ctx context.Context, path string, offset int64, w io.Writer) error {
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(logsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCtx.Done():
			return nil
		case <-ticker.C:
			newOffset, err := readGrowth(path, offset, w)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			offset = newOffset
		}
	}
}

// readGrowth reads and prints any bytes appended to the file at path since
// offset, returning the new offset. If the file is now smaller than
// offset (rotated/truncated), it reads from the start instead.
func readGrowth(path string, offset int64, w io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return offset, err
	}

	start := offset
	if fi.Size() < offset {
		start = 0
	}
	if fi.Size() == start {
		return fi.Size(), nil
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return offset, err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, f); err != nil {
		return offset, err
	}
	if buf.Len() > 0 {
		_, _ = io.WriteString(w, strings.TrimSuffix(buf.String(), "\n")+"\n")
	}
	return fi.Size(), nil
}
