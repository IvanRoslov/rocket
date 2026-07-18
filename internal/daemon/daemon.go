// Package daemon implements rocketd's lifecycle: pid-file locking, log
// rotation, and running the API server until asked to stop.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	_ "github.com/IvanRoslov/rocket/internal/agent/claudecode" // register the claude-code agent
	"github.com/IvanRoslov/rocket/internal/api"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/monitor"
	"github.com/IvanRoslov/rocket/internal/queue"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Run starts rocketd in the foreground: it claims the pid file, sets up
// rotating JSON logging, opens the store and bus, and serves the API until
// the process receives SIGTERM/SIGINT or a client calls POST /v1/shutdown.
// It returns an error if another daemon instance is already running.
func Run(cfg *config.Config) error {
	if err := claimPidFile(cfg.PidPath()); err != nil {
		return err
	}
	defer os.Remove(cfg.PidPath())

	logger := slog.New(slog.NewJSONHandler(&lumberjack.Logger{
		Filename:   cfg.LogPath(),
		MaxSize:    10, // megabytes
		MaxBackups: 3,
	}, nil))
	slog.SetDefault(logger)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	b := bus.New(st)
	rt := runtime.NewTmux()
	ws := workspace.New(cfg.WorktreesDir)
	mgr := session.NewManager(st, b, rt, ws, cfg)
	mon := monitor.New(st, b, rt, cfg, agent.Get)
	q := queue.New(st, b, rt, cfg, mon.Activity)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Reconcile store vs live sessions/worktrees before serving
	if err := mgr.Reconcile(ctx); err != nil {
		slog.Error("reconcile failed (non-fatal)", "error", err)
	}

	go mon.Run(ctx)

	// Recover synchronously (ResetDelivering + initial Wakes) BEFORE serving
	// the API: otherwise an early POST /v1/messages could Wake a recipient
	// whose delivery worker races the recovery pass. See Queue.Recover.
	q.Recover(ctx)
	go q.Run(ctx)

	shutdownCalled := make(chan struct{})
	var shutdownOnce func()
	shutdownOnce = func() {
		select {
		case <-shutdownCalled:
			// already shutting down
		default:
			close(shutdownCalled)
			stop()
		}
	}

	deps := api.Deps{
		Store:     st,
		Bus:       b,
		Cfg:       cfg,
		Manager:   mgr,
		Monitor:   mon,
		Queue:     q,
		Shutdown:  shutdownOnce,
		StartedAt: time.Now(),
	}

	slog.Info("rocketd starting", "pid", os.Getpid(), "socket", cfg.SocketPath())
	err = api.Serve(ctx, deps)
	slog.Info("rocketd stopped")
	return err
}

// claimPidFile creates the pid file exclusively, writing this process's pid.
// If a pid file already exists, it checks whether that pid is alive: if so,
// it's a genuine conflict; if not, the stale file is removed and the claim
// is retried once.
func claimPidFile(path string) error {
	pid := os.Getpid()
	err := writePidFileExclusive(path, pid)
	if err == nil {
		return nil
	}
	if !os.IsExist(err) {
		return fmt.Errorf("write pid file: %w", err)
	}

	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		return fmt.Errorf("read existing pid file: %w", readErr)
	}
	existingPid, parseErr := strconv.Atoi(string(existing))
	if parseErr == nil && processAlive(existingPid) {
		return fmt.Errorf("already running (pid %d)", existingPid)
	}

	// Stale pid file: remove it and retry once.
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale pid file: %w", err)
	}
	if err := writePidFileExclusive(path, pid); err != nil {
		if os.IsExist(err) {
			// Lost the race: another process claimed the pid file between
			// our removal and retry. Re-read it and report a genuine
			// conflict if a live pid is now present.
			if raced, readErr := os.ReadFile(path); readErr == nil {
				if racedPid, parseErr := strconv.Atoi(string(raced)); parseErr == nil && processAlive(racedPid) {
					return fmt.Errorf("already running (pid %d)", racedPid)
				}
			}
		}
		return fmt.Errorf("write pid file after removing stale one: %w", err)
	}
	return nil
}

func writePidFileExclusive(path string, pid int) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strconv.Itoa(pid))
	return err
}

// processAlive reports whether pid refers to a live process, using signal 0
// which performs existence/permission checks without actually signaling.
func processAlive(pid int) bool {
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
