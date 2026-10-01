package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Brainstorm quality metric (task #4901, spec §4): read-only, open to any
// caller. The numbers are computed by store.BrainstormStats on every read.

const (
	defaultStatsWeeks = 12
	maxStatsWeeks     = 520
)

type brainstormWeekResponse struct {
	Week                string `json:"week"`
	Skill               string `json:"skill"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
}

// brainstormStormResponse is one storm; go_at is unix seconds, null before Go.
type brainstormStormResponse struct {
	TaskID              int64  `json:"task_id"`
	Title               string `json:"title"`
	ProjectID           string `json:"project_id"`
	Skill               string `json:"skill"`
	Questions           int    `json:"questions"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
	SpecChanges         int    `json:"spec_changes"`
	GoAt                *int64 `json:"go_at"`
}

type brainstormStatsResponse struct {
	Weeks  []brainstormWeekResponse  `json:"weeks"`
	Storms []brainstormStormResponse `json:"storms"`
}

func toBrainstormStormResponse(s store.BrainstormStorm) brainstormStormResponse {
	return brainstormStormResponse{
		TaskID:              s.TaskID,
		Title:               s.Title,
		ProjectID:           s.ProjectID,
		Skill:               s.Skill,
		Questions:           s.Questions,
		Answered:            s.Answered,
		Accepted:            s.Accepted,
		AcceptedWithComment: s.AcceptedWithComment,
		Corrected:           s.Corrected,
		WrongTurn:           s.WrongTurn,
		SpecChanges:         s.SpecChanges,
		GoAt:                s.GoAt,
	}
}

func registerStatsRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/stats/brainstorm", func(w http.ResponseWriter, r *http.Request) {
		handleBrainstormStats(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/brainstorm/stats", func(w http.ResponseWriter, r *http.Request) {
		handleTaskBrainstormStats(w, r, d)
	})
}

// handleBrainstormStats serves the metric over the last ?weeks=N ISO weeks
// (default 12), cut in the daemon's local time.
func handleBrainstormStats(w http.ResponseWriter, r *http.Request, d Deps) {
	weeks := defaultStatsWeeks
	if v := r.URL.Query().Get("weeks"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxStatsWeeks {
			writeErr(w, http.StatusBadRequest, "bad_request",
				"weeks must be an integer from 1 to "+strconv.Itoa(maxStatsWeeks))
			return
		}
		weeks = n
	}
	stats, err := d.Store.BrainstormStats(time.Now(), weeks)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := brainstormStatsResponse{
		Weeks:  make([]brainstormWeekResponse, len(stats.Weeks)),
		Storms: make([]brainstormStormResponse, len(stats.Storms)),
	}
	for i, wk := range stats.Weeks {
		out.Weeks[i] = brainstormWeekResponse{
			Week:                wk.Week,
			Skill:               wk.Skill,
			Answered:            wk.Answered,
			Accepted:            wk.Accepted,
			AcceptedWithComment: wk.AcceptedWithComment,
			Corrected:           wk.Corrected,
			WrongTurn:           wk.WrongTurn,
		}
	}
	for i, s := range stats.Storms {
		out.Storms[i] = toBrainstormStormResponse(s)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTaskBrainstormStats serves one task's storm; zeros when it has none.
func handleTaskBrainstormStats(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := getTaskOr404(w, d, id); !ok {
		return
	}
	storm, err := d.Store.TaskBrainstormStats(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toBrainstormStormResponse(storm))
}
