package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	mux.HandleFunc("GET /v1/stats/prices", func(w http.ResponseWriter, r *http.Request) {
		handleUsagePrices(w, d)
	})
	mux.HandleFunc("PUT /v1/stats/prices/{model}", func(w http.ResponseWriter, r *http.Request) {
		handlePutUsagePrice(w, r, d)
	})
	mux.HandleFunc("DELETE /v1/stats/prices/{model}", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteUsagePrice(w, r, d)
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

type modelPriceResponse struct {
	Model      string   `json:"model"`
	Input      *float64 `json:"input"`
	CacheWrite *float64 `json:"cache_write"`
	CacheRead  *float64 `json:"cache_read"`
	Output     *float64 `json:"output"`
	UpdatedAt  *int64   `json:"updated_at"`
}

func toModelPriceResponse(p store.ModelPrice) modelPriceResponse {
	updated := p.UpdatedAt
	return modelPriceResponse{
		Model: p.Model, Input: p.Input, CacheWrite: p.CacheWrite,
		CacheRead: p.CacheRead, Output: p.Output, UpdatedAt: &updated,
	}
}

func handleUsagePrices(w http.ResponseWriter, d Deps) {
	prices, err := d.Store.ListModelPrices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	models, err := d.Store.UsageModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	byModel := make(map[string]modelPriceResponse, len(models)+len(prices))
	for _, model := range models {
		byModel[model] = modelPriceResponse{Model: model}
	}
	for _, p := range prices {
		byModel[p.Model] = toModelPriceResponse(p)
	}
	out := make([]modelPriceResponse, 0, len(byModel))
	for _, p := range byModel {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	writeJSON(w, http.StatusOK, struct {
		Prices []modelPriceResponse `json:"prices"`
	}{Prices: out})
}

// decodeUsagePrice requires all four rates. A rate may be null, but an
// omitted key would silently clear it, so partial updates belong in the CLI's
// read/merge/PUT flow rather than this replace endpoint.
func decodeUsagePrice(w http.ResponseWriter, r *http.Request, model string) (store.ModelPrice, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&fields); err != nil || fields == nil {
		return store.ModelPrice{}, fmt.Errorf("invalid JSON price body")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return store.ModelPrice{}, fmt.Errorf("price body must contain one JSON object")
	}
	if len(fields) != 4 {
		return store.ModelPrice{}, fmt.Errorf("price body requires input, cache_write, cache_read and output")
	}
	p := store.ModelPrice{Model: model, UpdatedAt: time.Now().Unix()}
	for key, dest := range map[string]**float64{
		"input": &p.Input, "cache_write": &p.CacheWrite,
		"cache_read": &p.CacheRead, "output": &p.Output,
	} {
		raw, ok := fields[key]
		if !ok {
			return store.ModelPrice{}, fmt.Errorf("missing %s rate", key)
		}
		if string(raw) == "null" {
			continue
		}
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return store.ModelPrice{}, fmt.Errorf("%s rate must be a nonnegative number or null", key)
		}
		*dest = &v
	}
	return p, nil
}

func handlePutUsagePrice(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	model := r.PathValue("model")
	if strings.TrimSpace(model) == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "model is required")
		return
	}
	p, err := decodeUsagePrice(w, r, model)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := d.Store.UpsertModelPrice(p); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toModelPriceResponse(p))
}

func handleDeleteUsagePrice(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	model := r.PathValue("model")
	if strings.TrimSpace(model) == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "model is required")
		return
	}
	if err := d.Store.DeleteModelPrice(model); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
