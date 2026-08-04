// Package daemon implements rocketd's lifecycle: pid-file locking, log
// rotation, and running the API server until asked to stop.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	_ "github.com/IvanRoslov/rocket/internal/agent/claudecode" // register the claude-code agent
	_ "github.com/IvanRoslov/rocket/internal/agent/codex"      // register the codex agent
	"github.com/IvanRoslov/rocket/internal/agentwatch"
	"github.com/IvanRoslov/rocket/internal/api"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/ghpoller"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/heartbeat"
	"github.com/IvanRoslov/rocket/internal/mirror"
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

	// Wire up the GitHub token source for session environment injection.
	// The token is read fresh on each spawn/restore call, so it can be updated
	// via `rocket github auth` without requiring a daemon restart.
	mgr.SetTokenSource(func() string {
		token, err := st.GetSetting("github_token")
		if err != nil {
			return ""
		}
		return token
	})

	mon := monitor.New(st, b, rt, cfg, agent.Get)
	q := queue.New(st, b, rt, cfg, mon.Activity)
	hb := heartbeat.New(st, b, cfg, mon.Activity, q.Wake)
	ms := mirror.NewSyncer(st, cfg)
	agentWatch := agentwatch.New(st, rt, cfg, mgr, q.Wake)
	reactions := ghpoller.NewReactions(st, b, q.Wake, mgr, mon.Activity, cfg)
	defer reactions.Stop()
	if err := reactions.RearmPending(); err != nil {
		slog.Error("rearm pending merge-grace timers failed (non-fatal)", "error", err)
	}
	ghp := ghpoller.New(st, b, githubClientFactory(st, cfg), cfg, reactions)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Reconcile store vs live sessions/worktrees before serving
	if err := mgr.Reconcile(ctx); err != nil {
		slog.Error("reconcile failed (non-fatal)", "error", err)
	}

	// Sweep activity synchronously once, after reconcile and BEFORE
	// q.Recover: without this, Recover's startup Wakes can race stale store
	// activity left over from before a restart (e.g. a session still marked
	// "active" from the moment of a crash), making delivery wait a full poll
	// cycle before re-checking. Run below still does its own initial sweep;
	// that duplicate is harmless.
	mon.SweepOnce(ctx)
	go mon.Run(ctx)

	// Recover synchronously (ResetDelivering + initial Wakes) BEFORE serving
	// the API: otherwise an early POST /v1/messages could Wake a recipient
	// whose delivery worker races the recovery pass. See Queue.Recover.
	q.Recover(ctx)
	go q.Run(ctx)

	go hb.Run(ctx)
	// Agents are not spawned by rocket: the watcher discovers their tmux
	// sessions, keeps the session rows honest and tells a freshly appeared
	// agent how much unread mail is waiting for it.
	go agentWatch.Run(ctx)
	go ghp.Run(ctx)
	// Keep the shared repo mirrors under cfg.ReposDir fresh: agents read
	// those working trees directly, so without this sweep they serve
	// whatever commit the mirror was cloned at. The sweep never clobbers —
	// see internal/mirror.
	go ms.Run(ctx)

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
		GH:        githubClientFactory(st, cfg),
	}

	slog.Info("rocketd starting", "pid", os.Getpid(), "socket", cfg.SocketPath())
	err = api.Serve(ctx, deps)
	slog.Info("rocketd stopped")
	return err
}

// githubClientFactory returns a func suitable for api.Deps.GH: it reads the
// GitHub token from settings fresh on every call (so a token saved via
// `rocket github auth` takes effect without a daemon restart), returning
// github.ErrNoToken if none is configured.
func githubClientFactory(st *store.Store, cfg *config.Config) func() (*github.Client, error) {
	return func() (*github.Client, error) {
		token, err := st.GetSetting("github_token")
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, github.ErrNoToken
			}
			return nil, err
		}
		return github.New(cfg.GithubAPIBase, token), nil
	}
}

// stopWaitPollInterval bounds how often WaitForExit re-checks pid liveness.
const stopWaitPollInterval = 100 * time.Millisecond

// StopWaitTimeout bounds how long `rocket daemon stop` (via WaitForExit)
// waits for a daemon to actually exit after POST /v1/shutdown succeeds,
// before giving up and reporting an error.
const StopWaitTimeout = 15 * time.Second

// ReadPid reads and parses cfg's pid file. It returns an error (satisfying
// os.IsNotExist when the file is missing, e.g. no daemon has ever run
// against this home) if the pid file can't be read or its contents aren't a
// valid pid.
func ReadPid(cfg *config.Config) (int, error) {
	data, err := os.ReadFile(cfg.PidPath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("parse pid file %s: %w", cfg.PidPath(), err)
	}
	return pid, nil
}

// WaitForExit polls pid (using the same kill(pid, 0) liveness check
// claimPidFile uses to detect a stale pid file) until it is no longer
// alive, returning nil. If pid is still alive once timeout elapses, it
// returns an error.
//
// `rocket daemon stop` calls this after POST /v1/shutdown succeeds: the
// shutdown handler responds to the client before Run's graceful sweep and
// socket/pid-file cleanup actually finish (see Run and api.Serve), so
// without waiting here, a `daemon start` launched immediately after `stop`
// returns can race a socket/pid file that hasn't been removed yet and fail
// with "daemon unavailable" (this was a recurring papercut with `make
// restart`, which used to paper over it with a fixed `sleep 2`).
func WaitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon (pid %d) did not exit within %s", pid, timeout)
		}
		time.Sleep(stopWaitPollInterval)
	}
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
