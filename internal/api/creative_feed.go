package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

func registerCreativeFeedRoutes(mux *http.ServeMux, d Deps) {
	for _, route := range []struct{ pattern, action string }{
		{"GET /v1/creative/runs/{run}/feed", "list"},
		{"POST /v1/creative/runs/{run}/feed", "post"},
		{"POST /v1/creative/runs/{run}/feed/cards", "propose"},
		{"GET /v1/creative/runs/{run}/feed/cards/{entry}", "card"},
		{"POST /v1/creative/runs/{run}/feed/cards/{entry}/decision", "decide"},
		{"GET /v1/creative/runs/{run}/feed/unread", "unread"},
		{"POST /v1/creative/runs/{run}/feed/read", "read"},
	} {
		mux.HandleFunc(route.pattern, creativeRoute(d, func(w http.ResponseWriter, r *http.Request, caller *store.Session) {
			handleCreativeFeed(w, r, d, caller, route.action)
		}))
	}
}
func creativeFeedError(w http.ResponseWriter, err error) {
	var conflict *store.CreativeFeedConflict
	if errors.As(err, &conflict) {
		writeJSON(w, 409, map[string]any{"error": map[string]string{"code": "creative_card_conflict", "message": err.Error()}, "entry": conflict.Entry})
		return
	}
	creativeError(w, err)
}
func creativeFeedAccess(w http.ResponseWriter, d Deps, caller *store.Session, runID int64, write bool) bool {
	if caller == nil {
		return true
	}
	own, err := d.Store.GetCreativeSessionContext(caller.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		creativeError(w, err)
		return false
	}
	if err != nil || own.RunID != runID {
		hint := "read your assigned run with rocket creative agent feed RUN"
		if err == nil {
			hint = fmt.Sprintf("read your own feed with rocket creative agent feed %d", own.RunID)
		}
		writeErr(w, 403, "creative_feed_forbidden", "caller must belong to this run; "+hint)
		return false
	}
	if !write {
		return true
	}
	run, err := d.Store.GetCreativeRun(runID)
	if err != nil {
		creativeError(w, err)
		return false
	}
	if caller.Kind != "orchestrator" || run.OrchestratorSessionID != caller.ID {
		writeErr(w, 403, "creative_feed_forbidden", "only the current run orchestrator writes to the human; message it with "+creativeFeedWorkerHint(own))
		return false
	}
	return true
}
func creativeFeedHuman(w http.ResponseWriter, caller *store.Session, runID int64) bool {
	if caller == nil {
		return true
	}
	writeErr(w, 403, "human_only", fmt.Sprintf("only the human may decide cards or mark the feed read; inspect state with rocket creative agent feed %d", runID))
	return false
}
func handleCreativeFeed(w http.ResponseWriter, r *http.Request, d Deps, caller *store.Session, action string) {
	runID, ok := creativeID(w, r.PathValue("run"), false)
	if !ok {
		return
	}
	if !creativeFeedAccess(w, d, caller, runID, action == "post" || action == "propose") {
		return
	}
	if (action == "decide" || action == "read") && !creativeFeedHuman(w, caller, runID) {
		return
	}
	if action == "propose" && caller == nil {
		writeErr(w, 403, "creative_feed_forbidden", fmt.Sprintf("only the current orchestrator publishes proposals; ask it in the feed with rocket creative agent feed post %d --body 'Your request'", runID))
		return
	}
	b := store.CreativeBinding{RunID: runID}
	switch action {
	case "list":
		for name, target := range map[string]*int64{"stage_id": &b.StageID, "item_id": &b.ItemID} {
			value := r.URL.Query().Get(name)
			if value == "" {
				continue
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				writeErr(w, 400, "creative_invalid", name+" must be nonnegative")
				return
			}
			*target = n
		}
		out, err := d.Store.ListCreativeFeed(b)
		if err != nil {
			creativeFeedError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case "unread":
		out, err := d.Store.CreativeFeedUnread(runID)
		if err != nil {
			creativeFeedError(w, err)
			return
		}
		writeJSON(w, 200, out)
	case "read":
		var in struct {
			StageID int64  `json:"stage_id"`
			Through *int64 `json:"through_entry_id"`
		}
		if !creativeBody(w, r, &in) {
			return
		}
		if in.Through == nil {
			writeErr(w, 400, "creative_invalid", "through_entry_id is required")
			return
		}
		b.StageID = in.StageID
		cursor, err := d.Store.MarkCreativeFeedRead(b, *in.Through)
		if err != nil {
			creativeFeedError(w, err)
			return
		}
		writeJSON(w, 200, map[string]int64{"stage_id": b.StageID, "through_entry_id": cursor})
	case "card", "decide":
		id, ok := creativeID(w, r.PathValue("entry"), false)
		if !ok {
			return
		}
		var e store.CreativeFeedEntry
		var err error
		if action == "card" {
			e, err = d.Store.GetCreativeFeedCard(runID, id)
		} else {
			var in struct {
				Decision string `json:"decision"`
			}
			if !creativeBody(w, r, &in) {
				return
			}
			e, err = d.Store.DecideCreativeFeedCard(runID, id, in.Decision)
		}
		if err != nil {
			creativeFeedError(w, err)
			return
		}
		if action == "decide" {
			drainCreativeFeed(d, runID)
		}
		writeJSON(w, 200, e)
	case "post", "propose":
		var in struct {
			StageID  int64   `json:"stage_id"`
			ItemID   int64   `json:"item_id"`
			Body     string  `json:"body"`
			Versions []int64 `json:"artifact_version_ids"`
			Replaces int64   `json:"replaces_entry_id"`
		}
		if !creativeBody(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.Body) == "" {
			writeErr(w, 400, "empty_body", "body must not be empty; use --body TEXT or --file PATH")
			return
		}
		if action == "post" && in.Replaces != 0 {
			writeErr(w, 400, "creative_invalid", "replacement requires rocket creative agent feed propose RUN --replaces CARD")
			return
		}
		author, kind, sessionID := "human", "message", ""
		if caller != nil {
			author = "orchestrator"
			sessionID = caller.ID
		}
		if action == "propose" {
			kind = "proposal"
		}
		e, err := d.Store.AddCreativeFeedEntry(store.CreativeFeedEntry{RunID: runID, StageID: in.StageID, ItemID: in.ItemID, Kind: kind, Author: author, AuthorSessionID: sessionID, Body: in.Body, ArtifactVersionIDs: in.Versions}, in.Replaces)
		if err != nil {
			creativeFeedError(w, err)
			return
		}
		if author == "human" {
			drainCreativeFeed(d, runID)
		}
		writeJSON(w, 201, e)
	}
}
func drainCreativeFeed(d Deps, runID int64) {
	ids, err := d.Store.QueueCreativeFeedDeliveries(runID)
	if err != nil {
		// The human mutation has committed with its durable outbox. Launch/restore
		// retries draining; never report a failed send that would invite duplicates.
		if d.Bus != nil {
			d.Bus.Publish("creative.feed_delivery_pending", "", map[string]any{"run_id": runID, "error": err.Error()})
		}
		return
	}
	for _, id := range ids {
		m, err := d.Store.GetMessage(id)
		if err == nil {
			wakeCreativeMessage(d, id, m.ToSession)
		}
	}
}

func creativeFeedWorkerHint(b store.CreativeBinding) string {
	cmd := fmt.Sprintf("rocket creative agent send %d", b.RunID)
	if b.StageID != 0 {
		cmd += fmt.Sprintf(" --stage %d", b.StageID)
	}
	if b.ItemID != 0 {
		cmd += fmt.Sprintf(" --item %d", b.ItemID)
	}
	return cmd + " --body 'Your message'"
}
