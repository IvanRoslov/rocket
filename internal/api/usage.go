package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/usage"
)

const usageDateFormat = "2006-01-02"

// usageDateWindow converts inclusive local calendar dates to the store's
// half-open Unix-second interval. AddDate preserves local midnights over DST.
func usageDateWindow(fromText, toText string, now time.Time, loc *time.Location) (from, to int64, fromDay, toDay string, err error) {
	if loc == nil {
		loc = time.Local
	}
	if toText == "" {
		toText = now.In(loc).Format(usageDateFormat)
	}
	toDate, err := time.ParseInLocation(usageDateFormat, toText, loc)
	if err != nil || toDate.Format(usageDateFormat) != toText {
		return 0, 0, "", "", fmt.Errorf("to must be YYYY-MM-DD")
	}
	if fromText == "" {
		fromText = toDate.AddDate(0, 0, -29).Format(usageDateFormat)
	}
	fromDate, err := time.ParseInLocation(usageDateFormat, fromText, loc)
	if err != nil || fromDate.Format(usageDateFormat) != fromText {
		return 0, 0, "", "", fmt.Errorf("from must be YYYY-MM-DD")
	}
	if fromDate.After(toDate) {
		return 0, 0, "", "", fmt.Errorf("from must not be after to")
	}
	return fromDate.Unix(), toDate.AddDate(0, 0, 1).Unix(), fromText, toText, nil
}

type usagePeriodResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
	usage.PeriodSummary
	Pending int `json:"pending"`
}

func registerUsageRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/stats/usage", func(w http.ResponseWriter, r *http.Request) {
		handleUsagePeriod(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/usage", func(w http.ResponseWriter, r *http.Request) {
		handleTaskUsage(w, r, d)
	})
}

func handleUsagePeriod(w http.ResponseWriter, r *http.Request, d Deps) {
	q := r.URL.Query()
	from, to, fromDay, toDay, err := usageDateWindow(q.Get("from"), q.Get("to"), time.Now(), time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	project := q.Get("project")
	rows, err := d.Store.UsageRows(from, to, project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	prices, err := d.Store.ListModelPrices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	pending, err := d.Store.CountPendingUsage(from, to, project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usagePeriodResponse{
		From: fromDay, To: toDay, PeriodSummary: usage.Period(rows, prices), Pending: pending,
	})
}

func handleTaskUsage(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	task, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	if task.ParentID != 0 {
		writeErr(w, http.StatusBadRequest, "not_root_task", "usage is available for root tasks only")
		return
	}
	rows, err := d.Store.TaskUsageRows(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	prices, err := d.Store.ListModelPrices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage.Task(rows, prices, id, prURLForRepo(r, d.Store)))
}

// prURLForRepo resolves each repo's remote at most once per request and
// returns an empty URL when a repo is missing or is not hosted on GitHub.
func prURLForRepo(r *http.Request, st *store.Store) func(string, int) string {
	baseByRepo := make(map[string]string)
	return func(repoID string, number int) string {
		if repoID == "" || number <= 0 {
			return ""
		}
		base, seen := baseByRepo[repoID]
		if !seen {
			baseByRepo[repoID] = ""
			repo, err := st.GetRepo(repoID)
			if err == nil {
				remote, err := gitRemoteOrigin(r.Context(), repo.Path)
				if err == nil {
					owner, name, ok := github.ParseRemote(remote)
					if ok {
						base = "https://github.com/" + owner + "/" + name + "/pull/"
						baseByRepo[repoID] = base
					}
				}
			}
		}
		if base == "" {
			return ""
		}
		return base + strconv.Itoa(number)
	}
}
