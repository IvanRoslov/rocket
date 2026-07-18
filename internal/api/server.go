// Package api implements rocket's HTTP API, served over both a unix socket
// and a localhost TCP listener.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/version"
)

// shutdownTimeout bounds how long Serve waits for in-flight requests to
// finish during graceful shutdown.
const shutdownTimeout = 5 * time.Second

// Deps holds the dependencies the API handler needs. This struct's shape is
// load-bearing for callers and must not be reshuffled lightly.
type Deps struct {
	Store     *store.Store
	Bus       *bus.Bus
	Cfg       *config.Config
	Manager   *session.Manager
	Shutdown  func()
	StartedAt time.Time
}

// NewHandler builds the routed http.Handler for rocket's API.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": version.Version,
			"uptime":  time.Since(d.StartedAt).String(),
		})
	})

	mux.HandleFunc("POST /v1/shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "shutting_down",
		})
		// Run asynchronously so the response above has a chance to flush
		// before the server (and process) starts shutting down.
		go d.Shutdown()
	})

	registerRepoRoutes(mux, d)
	registerProjectRoutes(mux, d)
	registerSessionRoutes(mux, d)
	registerEventsRoutes(mux, d)

	// Catch-all: anything not matched by a more specific pattern above is a
	// 404, rendered in the standard error JSON shape.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "resource not found")
	})

	return mux
}

// Serve listens on both d.Cfg.SocketPath() (a unix socket, mode 0600) and
// 127.0.0.1:<d.Cfg.Port>, serving the same handler on both. It blocks until
// ctx is cancelled, at which point it gracefully shuts down both servers,
// removes the socket file, and returns nil. If either listener fails to
// start, or either server exits with a fatal error before ctx is cancelled,
// Serve returns that error.
func Serve(ctx context.Context, d Deps) error {
	sockPath := d.Cfg.SocketPath()

	// Remove a stale socket file left behind by a previous, uncleanly
	// terminated process.
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	unixLn, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", sockPath, err)
	}
	if err := os.Chmod(sockPath, 0600); err != nil {
		unixLn.Close()
		os.Remove(sockPath)
		return fmt.Errorf("chmod socket: %w", err)
	}

	tcpAddr := fmt.Sprintf("127.0.0.1:%d", d.Cfg.Port)
	tcpLn, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		unixLn.Close()
		os.Remove(sockPath)
		return fmt.Errorf("listen tcp %s: %w", tcpAddr, err)
	}

	handler := NewHandler(d)
	unixSrv := &http.Server{Handler: handler}
	tcpSrv := &http.Server{Handler: handler}

	errCh := make(chan error, 2)
	go func() {
		if err := unixSrv.Serve(unixLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("unix server: %w", err)
		}
	}()
	go func() {
		if err := tcpSrv.Serve(tcpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("tcp server: %w", err)
		}
	}()

	var fatalErr error
	select {
	case <-ctx.Done():
		// graceful path below
	case fatalErr = <-errCh:
		// one of the servers died unexpectedly; still attempt a clean
		// shutdown of the other before returning.
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = unixSrv.Shutdown(shutdownCtx)
	_ = tcpSrv.Shutdown(shutdownCtx)
	os.Remove(sockPath)

	return fatalErr
}
