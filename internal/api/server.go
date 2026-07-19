// Package api implements rocket's HTTP API, served over both a unix socket
// and a localhost TCP listener.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/monitor"
	"github.com/IvanRoslov/rocket/internal/queue"
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
	Monitor   *monitor.Monitor
	Queue     *queue.Queue
	Shutdown  func()
	StartedAt time.Time

	// GH builds a GitHub API client using the currently stored token,
	// reading it fresh from settings on every call (so a token change takes
	// effect without a daemon restart). It returns github.ErrNoToken if no
	// token is configured.
	GH func() (*github.Client, error)
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
	registerGithubCatalogRoutes(mux, d)
	registerProjectRoutes(mux, d)
	registerSessionRoutes(mux, d)
	registerChatRoutes(mux, d)
	registerTermRoutes(mux, d)
	registerTaskRoutes(mux, d)
	registerEventsRoutes(mux, d)
	registerSSERoutes(mux, d)
	registerInternalActivityRoutes(mux, d)
	registerInternalQuizRoutes(mux, d)
	registerQuizRoutes(mux, d)
	registerMessageRoutes(mux, d)
	registerSystemRoutes(mux, d)
	registerQuestionRoutes(mux, d)
	registerSettingsRoutes(mux, d)

	// Any /v1 path not matched by a more specific route above is a 404,
	// rendered in the standard error JSON shape. This is a prefix pattern,
	// so it's less specific than the exact "GET /v1/health" etc. routes
	// registered above and only catches the leftovers.
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not_found", "resource not found")
	})

	// Everything else falls to the embedded dashboard static build, with a
	// SPA fallback to index.html for client-side routes.
	registerStaticRoutes(mux)

	return mux
}

// Serve listens on both d.Cfg.SocketPath() (a unix socket, mode 0600) and
// <d.Cfg.Host>:<d.Cfg.Port> (127.0.0.1 by default; set host in config.yaml
// to expose the API on the LAN, e.g. for the mobile app), serving the same
// handler on both. It blocks until
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

	tcpAddr := fmt.Sprintf("%s:%d", d.Cfg.Host, d.Cfg.Port)
	tcpLn, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		unixLn.Close()
		os.Remove(sockPath)
		return fmt.Errorf("listen tcp %s: %w", tcpAddr, err)
	}

	handler := NewHandler(d)
	unixSrv := &http.Server{Handler: handler}
	tcpSrv := &http.Server{Handler: handler}
	var tlsSrv *http.Server

	errCh := make(chan error, 3)
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

	// Optional https listener (tls_port, 0 = off). Its whole point is
	// HTTP/2: browsers cap cleartext HTTP/1.1 at ~6 connections per host,
	// which the dashboard's long-lived SSE streams exhaust (page loads then
	// stall in the queue); over TLS net/http negotiates h2 via ALPN
	// automatically and everything multiplexes onto one connection.
	if d.Cfg.TLSPort > 0 {
		certFile, keyFile, created, err := EnsureTLSCert(filepath.Join(d.Cfg.Home, "tls"), d.Cfg.Host)
		if err != nil {
			slog.Error("tls: ensure certificate failed, https listener disabled", "error", err)
		} else {
			tlsAddr := fmt.Sprintf("%s:%d", d.Cfg.Host, d.Cfg.TLSPort)
			tlsLn, err := net.Listen("tcp", tlsAddr)
			if err != nil {
				slog.Error("tls: listen failed, https listener disabled", "addr", tlsAddr, "error", err)
			} else {
				tlsSrv = &http.Server{Handler: handler}
				if created {
					slog.Info("tls: generated self-signed certificate; trust it once to silence the browser warning (or replace with an mkcert pair)",
						"cert", certFile)
				}
				slog.Info("tls: https listener up (HTTP/2)", "addr", "https://"+tlsAddr)
				go func() {
					if err := tlsSrv.ServeTLS(tlsLn, certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
						errCh <- fmt.Errorf("tls server: %w", err)
					}
				}()
			}
		}
	}

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
	if tlsSrv != nil {
		_ = tlsSrv.Shutdown(shutdownCtx)
	}
	os.Remove(sockPath)

	return fatalErr
}
