package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// defaultChatLimit and maxChatLimit bound the "limit" query parameter on
// GET /v1/sessions/{id}/chat.
const (
	defaultChatLimit = 200
	maxChatLimit     = 1000
)

// chatEntryResponse is the JSON shape of one chat entry as returned by the
// API.
type chatEntryResponse struct {
	Role     string `json:"role"`
	Text     string `json:"text"`
	ToolName string `json:"tool_name,omitempty"`
	TS       int64  `json:"ts"`
}

func toChatEntryResponse(e agent.ChatEntry) chatEntryResponse {
	return chatEntryResponse{
		Role:     e.Role,
		Text:     e.Text,
		ToolName: e.ToolName,
		TS:       e.TS,
	}
}

// registerChatRoutes wires the /v1/sessions/{id}/chat route onto mux.
func registerChatRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/sessions/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		handleSessionChat(w, r, d)
	})
}

// handleSessionChat serves GET /v1/sessions/{id}/chat?cursor=&limit=.
//
// cursor=="" returns only the last `limit` entries of the transcript (tail
// semantics), for the initial load; cursor!="" normally returns ALL entries
// the agent reports past that cursor, unsliced (see
// docs/superpowers/specs/2026-07-19-session-chat-design.md and the task-3
// binding resolutions): incremental cursor-based reads are expected to be
// small by construction, so no further slicing/cursor-recomputation is done
// for them in the common case. However, the same `limit` cap is always
// applied as a backstop regardless of cursor: an invalid/stale cursor (e.g.
// the transcript file was deleted/rotated, or a client-supplied cursor the
// adapter rejected as untrusted) makes the adapter fall back to reading
// from byte 0 of the current transcript, which without this cap would
// return the entire transcript history in one response.
//
// If the session's agent is unknown (e.g. a stale/removed adapter), this
// responds 200 with an empty entries list rather than failing the request,
// since the session itself is valid and still worth showing.
func handleSessionChat(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	sess, err := d.Store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit := defaultChatLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxChatLimit {
		limit = maxChatLimit
	}

	ag, err := agent.Get(sess.Agent)
	if err != nil {
		slog.Warn("api: unknown agent for session chat, returning empty entries", "session", sess.ID, "agent", sess.Agent, "error", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"entries":     []chatEntryResponse{},
			"next_cursor": "",
			"session":     toSessionRef(sess),
		})
		return
	}

	ref := agent.ActivityRef{SessionID: sess.ID, WorktreePath: sess.WorktreePath}
	entries, nextCursor, err := ag.TranscriptTail(r.Context(), ref, cursor)
	if err != nil {
		if errors.Is(err, agent.ErrNoSignal) {
			writeJSON(w, http.StatusOK, map[string]any{
				"entries":     []chatEntryResponse{},
				"next_cursor": "",
				"session":     toSessionRef(sess),
			})
			return
		}
		writeErr(w, http.StatusBadGateway, "transcript_error", err.Error())
		return
	}

	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	out := make([]chatEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = toChatEntryResponse(e)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries":     out,
		"next_cursor": nextCursor,
		"session":     toSessionRef(sess),
	})
}

// sessionRef is the minimal session shape embedded in the chat response.
type sessionRef struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Activity string `json:"activity,omitempty"`

	// PendingQuiz mirrors sessionResponse.PendingQuiz — see its doc comment.
	PendingQuiz *quizResponse `json:"pending_quiz,omitempty"`
}

func toSessionRef(s store.Session) sessionRef {
	return sessionRef{
		ID:          s.ID,
		Kind:        s.Kind,
		State:       s.State,
		Activity:    s.Activity,
		PendingQuiz: parseQuizResponse(s.PendingQuiz),
	}
}
