// The unified thread inbox (task #1023, spec v1 §«Единый инбокс»). Before it,
// "what is open and on whom" could only be answered by walking every task and
// every role in a loop — which is exactly what agents did, and why they missed
// threads. One endpoint answers it for task and role threads together.
package api

import (
	"errors"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// threadInboxEntry is one line of the inbox: enough to decide whether a thread
// needs you, without opening it. It deliberately carries the FIRST message
// only — the question body — rather than the whole thread; the per-thread
// endpoints stay the place to read a conversation.
type threadInboxEntry struct {
	// LocalRef is the one id a user types back: "1023/Q2" or "cto/Q1".
	LocalRef string `json:"local_ref"`
	// Kind is task|role; TaskID/RoleID carry whichever applies. Subject is the
	// human-readable rendering of the same thing.
	Kind    string `json:"kind"`
	TaskID  int64  `json:"task_id,omitempty"`
	RoleID  string `json:"role_id,omitempty"`
	Subject string `json:"subject"`
	// ID is the global question id, kept for clients that still address
	// threads by it.
	ID      int64  `json:"id"`
	Ordinal int    `json:"ordinal"`
	AskedBy string `json:"asked_by"`
	// Title is the thread's one-line heading — what a listing renders instead
	// of a truncated body.
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Status       string   `json:"status"`
	Resolution   string   `json:"resolution,omitempty"`
	Type         string   `json:"type"`
	Options      []string `json:"options,omitempty"`
	Participants []string `json:"participants"`
	Attention    []string `json:"attention"`
	// WaitingOn is Attention under its original name, so a client written
	// against the per-thread shape can read this listing unchanged.
	WaitingOn []string `json:"waiting_on"`
	YourTurn  bool     `json:"your_turn"`
	AskedAt   int64    `json:"asked_at"`
	// UpdatedAt is when the thread last moved — the last entry, or the
	// question itself in a thread nobody has answered. It is what "how long
	// has this been hanging" is measured from.
	UpdatedAt  int64 `json:"updated_at"`
	ResolvedAt int64 `json:"resolved_at,omitempty"`
	// Stale mirrors questionResponse.Stale: an open decision thread nobody has
	// moved for longer than question_stale_after. The inbox is the one screen
	// that shows every thread at once, so the badge has to be readable here
	// rather than only after opening each thread — and the threshold is
	// configurable, so a client cannot honestly recompute it.
	Stale bool `json:"stale,omitempty"`
	// ProjectID and TaskTitle give a task thread the context the per-task
	// endpoints get for free from their URL: a dashboard row links to the task
	// and labels itself with the title. Subject is a human-readable sentence
	// and must not be parsed back apart for them. Both are empty on a role
	// thread, which hangs off no project.
	ProjectID string `json:"project_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
}

func registerThreadInboxRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/threads", func(w http.ResponseWriter, r *http.Request) {
		handleGetThreads(w, r, d)
	})
}

// handleGetThreads serves GET /v1/threads?all=true&waiting_on=<id>: every
// thread the caller may read, task and role alike, newest subject first.
//
// Permission is the per-thread canReadThread, applied entry by entry: a thread
// the caller could not open individually is skipped rather than refused, so
// the inbox of a restricted caller is simply shorter.
func handleGetThreads(w http.ResponseWriter, r *http.Request, d Deps) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

	includeResolved := r.URL.Query().Get("all") == "true"
	waitingOn := r.URL.Query().Get("waiting_on")

	threads, err := d.Store.ListThreads(includeResolved)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ordinals, err := d.Store.ThreadOrdinals()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Task titles and orchestrator ids are looked up once per task, not once
	// per thread: a busy task can easily carry a dozen threads.
	tasks := map[int64]store.Task{}
	out := make([]threadInboxEntry, 0, len(threads))
	for _, th := range threads {
		q := th.Question

		if waitingOn != "" && !contains(th.Attention, waitingOn) {
			continue
		}

		subj := threadSubject{TaskID: q.TaskID, RoleID: q.RoleID}
		title := ""
		projectID := ""
		if q.TaskID != 0 {
			task, ok := tasks[q.TaskID]
			if !ok {
				task, err = d.Store.GetTask(q.TaskID)
				if errors.Is(err, store.ErrNotFound) {
					// A thread whose task is gone is not something anybody can
					// act on; skipping it beats failing the whole listing.
					continue
				}
				if err != nil {
					writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
					return
				}
				tasks[q.TaskID] = task
			}
			subj.Counterpart = task.SessionID
			title = task.Title
			projectID = task.ProjectID
		} else {
			subj.Counterpart = q.RoleID
		}

		if !canReadThread(d, caller, subj, th.Participants) {
			continue
		}

		kind := "task"
		if q.RoleID != "" {
			kind = "role"
		}
		updatedAt := q.AskedAt
		if th.LastMessage != nil {
			updatedAt = th.LastMessage.CreatedAt
		}
		out = append(out, threadInboxEntry{
			LocalRef:   threadLocalRef(subj, ordinals[q.ID]),
			Kind:       kind,
			TaskID:     q.TaskID,
			RoleID:     q.RoleID,
			Subject:    threadSubjectLabel(subj, title),
			ID:         q.ID,
			Ordinal:    ordinals[q.ID],
			AskedBy:    wireParticipant(q.AskedBy),
			Title:      q.Title,
			Body:       q.Body,
			Status:     q.Status,
			Resolution: q.Resolution,
			Type:       q.Type,
			Options:    q.Options,
			// emptyIfNil on every list field without omitempty: a nil Go slice
			// marshals to `null`, and these are documented as "always present,
			// possibly empty" — the dashboard calls .filter() on `attention`
			// straight off the wire. An empty attention set is not an edge
			// case, every resolved thread has one. `options` is omitempty, so
			// nil and empty alike drop out and the client reads it as optional.
			Participants: emptyIfNil(th.Participants),
			Attention:    emptyIfNil(th.Attention),
			WaitingOn:    emptyIfNil(th.Attention),
			YourTurn:     contains(th.Attention, callerParticipant(caller)),
			AskedAt:      q.AskedAt,
			UpdatedAt:    updatedAt,
			ResolvedAt:   q.ResolvedAt,
			Stale:        threadStale(d, q, th.LastMessage, th.Attention),
			ProjectID:    projectID,
			TaskTitle:    title,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": out})
}
