package api

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/version"
)

// maxLogTailLines and maxLogTailBytes bound the log_tail returned by
// GET /v1/system: at most this many lines, read from at most this many
// bytes at the end of the log file, so a large rocketd.log can't blow up
// response size or latency.
const (
	maxLogTailLines = 200
	maxLogTailBytes = 64 * 1024
)

// daemonInfo is the "daemon" section of GET /v1/system's response.
type daemonInfo struct {
	Version    string `json:"version"`
	UptimeS    int64  `json:"uptime_s"`
	Port       int    `json:"port"`
	Socket     string `json:"socket"`
	DBPath     string `json:"db_path"`
	ConfigPath string `json:"config_path"`
}

// queueInfo is the "queue" section of GET /v1/system's response.
type queueInfo struct {
	Queued int `json:"queued"`
	Failed int `json:"failed"`
}

// tmuxResponse is one entry of the "tmux" section of GET /v1/system's
// response.
type tmuxResponse struct {
	Name      string `json:"name"`
	SessionID string `json:"session_id,omitempty"`
	// State is the owning session's state (e.g. "running", "killed",
	// "errored"), omitted when Orphan is true.
	State  string `json:"state,omitempty"`
	Orphan bool   `json:"orphan"`
}

// worktreeResponse is one entry of the "worktrees" section of GET
// /v1/system's response.
type worktreeResponse struct {
	Path      string `json:"path"`
	SessionID string `json:"session_id,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	// State is the owning session's state (e.g. "running", "killed",
	// "errored"), omitted when Orphan is true.
	State  string `json:"state,omitempty"`
	Orphan bool   `json:"orphan"`
}

// systemResponse is the JSON shape of GET /v1/system.
type systemResponse struct {
	Daemon    daemonInfo         `json:"daemon"`
	Queue     queueInfo          `json:"queue"`
	Tmux      []tmuxResponse     `json:"tmux"`
	Worktrees []worktreeResponse `json:"worktrees"`
	LogTail   []string           `json:"log_tail"`
}

// registerSystemRoutes wires the /v1/system routes onto mux.
func registerSystemRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/system", func(w http.ResponseWriter, r *http.Request) {
		handleGetSystem(w, r, d)
	})
	mux.HandleFunc("POST /v1/system/cleanup", func(w http.ResponseWriter, r *http.Request) {
		handlePostSystemCleanup(w, r, d)
	})
}

func handleGetSystem(w http.ResponseWriter, r *http.Request, d Deps) {
	counts, err := d.Store.CountMessagesByStatus()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Each of these collectors inspects live system state (tmux, the
	// filesystem, the log file) that can fail independently of the
	// others and independently of the store query above. A single bad
	// collector shouldn't 500 the whole overview: log it and degrade to
	// an empty slice for that section so the rest of the response still
	// renders.
	tmux, err := d.Manager.ListTmux(r.Context())
	if err != nil {
		slog.Warn("api: GET /v1/system: list tmux failed, degrading to empty", "error", err)
		tmux = nil
	}

	worktrees, err := d.Manager.ListWorktrees()
	if err != nil {
		slog.Warn("api: GET /v1/system: list worktrees failed, degrading to empty", "error", err)
		worktrees = nil
	}

	logTail, err := readLogTail(d.Cfg.LogPath(), maxLogTailLines, maxLogTailBytes)
	if err != nil {
		slog.Warn("api: GET /v1/system: read log tail failed, degrading to empty", "error", err)
		logTail = nil
	}
	if logTail == nil {
		logTail = []string{}
	}

	tmuxOut := make([]tmuxResponse, len(tmux))
	for i, t := range tmux {
		tmuxOut[i] = tmuxResponse{Name: t.Name, SessionID: t.SessionID, State: t.State, Orphan: t.Orphan}
	}

	wtOut := make([]worktreeResponse, len(worktrees))
	for i, e := range worktrees {
		wtOut[i] = worktreeResponse{Path: e.Path, SessionID: e.SessionID, SizeBytes: e.SizeBytes, State: e.State, Orphan: e.Orphan}
	}

	writeJSON(w, http.StatusOK, systemResponse{
		Daemon: daemonInfo{
			Version:    version.Version,
			UptimeS:    int64(time.Since(d.StartedAt).Seconds()),
			Port:       d.Cfg.Port,
			Socket:     d.Cfg.SocketPath(),
			DBPath:     d.Cfg.DBPath(),
			ConfigPath: d.Cfg.ConfigPath(),
		},
		Queue: queueInfo{
			Queued: counts["queued"],
			Failed: counts["failed"],
		},
		Tmux:      tmuxOut,
		Worktrees: wtOut,
		LogTail:   logTail,
	})
}

func handlePostSystemCleanup(w http.ResponseWriter, r *http.Request, d Deps) {
	killed, removed, err := d.Manager.Cleanup(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if killed == nil {
		killed = []string{}
	}
	if removed == nil {
		removed = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"killed_tmux":       killed,
		"removed_worktrees": removed,
	})
}

// readLogTail returns the last maxLines lines found within the last
// maxBytes bytes of the file at path. A missing file is treated as empty,
// not an error.
func readLogTail(path string, maxLines int, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	text := string(data)

	if start > 0 {
		// We started reading mid-file: the first line is very likely a
		// truncated partial line, so drop it.
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			text = text[idx+1:]
		} else {
			text = ""
		}
	}

	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}, nil
	}

	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}
