package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

func lifecycleSetup(t *testing.T) (Deps, store.CreativeRun, string, int64) {
	t.Helper()
	d, r := creativeAPISetup(t)
	if err := d.Store.BindCreativeSession("orch", store.CreativeBinding{RunID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.SetCreativeRunOrchestrator(r.ID, "orch"); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/creative/runs/%d/stages/%d/actions", r.ID, r.Stages[0].ID)
	creativeRequest(t, d, "POST", path, `{"action":"start"}`, "orch", 200)
	pub := decodePublication(t, artifactRequest(NewHandler(d), "POST", creativePublishPath(r.ID, r.Stages[0].ID, "Result", "result", "plan.txt"), "text/plain", "orch", strings.NewReader("plan")))
	creativeRequest(t, d, "POST", path, fmt.Sprintf(`{"action":"submit","artifact_version_ids":[%d]}`, pub.Version.ID), "orch", 200)
	return d, r, path, pub.Version.ID
}
func lifecycleEvent(t *testing.T, d Deps, action string) store.Event {
	t.Helper()
	events, err := d.Store.ListEvents(0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Data["action"] == action {
			return events[i]
		}
	}
	t.Fatalf("no %s event", action)
	return store.Event{}
}
func TestCreativeLifecycleDecisionQueueExactSnapshot(t *testing.T) {
	d, r, path, version := lifecycleSetup(t)
	begin := time.Now().Unix()
	creativeRequest(t, d, "POST", path, `{"action":"approve","review_revision":99}`, "", 409)
	creativeRequest(t, d, "POST", path, `{"action":"approve","review_revision":1}`, "orch", 403)
	ids, err := d.Store.ListCreativeDiscussionIDs("message", store.CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID})
	if err != nil || len(ids) != 0 {
		t.Fatalf("premature message %v %v", ids, err)
	}
	creativeRequest(t, d, "POST", path, `{"action":"reject","review_revision":1}`, "", 200)
	// A new current orchestrator receives only subsequent decisions, no replay.
	addTestSession(t, d, "replacement", "orchestrator", "p")
	if err = d.Store.BindCreativeSession("replacement", store.CreativeBinding{RunID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if err = d.Store.SetCreativeRunOrchestrator(r.ID, "replacement"); err != nil {
		t.Fatal(err)
	}
	creativeRequest(t, d, "POST", path, fmt.Sprintf(`{"action":"submit","artifact_version_ids":[%d]}`, version), "orch", 200)
	creativeRequest(t, d, "POST", path, `{"action":"approve","review_revision":2}`, "", 200)
	ids, err = d.Store.ListCreativeDiscussionIDs("message", store.CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID})
	if err != nil || len(ids) != 2 {
		t.Fatalf("messages %v %v", ids, err)
	}
	for i, id := range ids {
		m, err := d.Store.GetMessage(id)
		if err != nil {
			t.Fatal(err)
		}
		to, decision := "orch", "reject"
		if i == 1 {
			to, decision = "replacement", "approve"
		}
		if m.ToSession != to || m.Status != "queued" || m.FromSession != "" {
			t.Fatalf("queue/attribution %+v", m)
		}
		lines := strings.Split(m.Body, "\n")
		var data map[string]any
		if len(lines) < 3 || json.Unmarshal([]byte(lines[2]), &data) != nil {
			t.Fatalf("missing decision metadata %s", m.Body)
		}
		for k, want := range map[string]any{"run_id": float64(r.ID), "stage_id": float64(r.Stages[0].ID), "item_id": float64(0), "action": decision, "review_revision": float64(i + 1), "actor_kind": "unattributed", "actor_source": "no_session_header", "actor_session_id": ""} {
			if data[k] != want {
				t.Errorf("%s=%v want %v", k, data[k], want)
			}
		}
		versions, _ := json.Marshal(data["artifact_version_ids"])
		if string(versions) != fmt.Sprintf("[%d]", version) {
			t.Fatalf("message versions %s", versions)
		}
		ts, ok := data["timestamp"].(float64)
		if !ok || int64(ts) < begin || int64(ts) > time.Now().Unix() {
			t.Fatalf("timestamp %v", data)
		}
		b, err := d.Store.GetCreativeDiscussionContext("message", id)
		if err != nil || len(b.ArtifactVersionIDs) != 1 || b.ArtifactVersionIDs[0] != version {
			t.Fatalf("snapshot %+v %v", b, err)
		}
		e := lifecycleEvent(t, d, decision)
		if e.Data["notification_status"] != "queued" || e.Data["notification_message_id"] != float64(id) {
			t.Fatalf("event %+v", e)
		}
		if i == 1 && !strings.Contains(m.Body, "rocket creative agent close") {
			t.Fatal("missing close reminder")
		}
	}
}
func TestCreativeLifecycleWithdrawFYIAndDenials(t *testing.T) {
	d, r, path, version := lifecycleSetup(t)
	addTestSession(t, d, "worker", "worker", "p")
	if err := d.Store.BindCreativeSession("worker", store.CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID}); err != nil {
		t.Fatal(err)
	}
	body := `{"action":"withdraw","review_revision":1,"reason":"Correct the publication"}`
	for _, caller := range []string{"", "worker", "outsider"} {
		creativeRequest(t, d, "POST", path, body, caller, 403)
	}
	creativeRequest(t, d, "POST", path, `{"action":"withdraw","review_revision":1,"reason":"  "}`, "orch", 400)
	creativeRequest(t, d, "POST", path, `{"action":"withdraw","review_revision":2,"reason":"Correction"}`, "orch", 409)
	creativeRequest(t, d, "POST", path, body, "orch", 200)
	creativeRequest(t, d, "POST", path, body, "orch", 409)
	ids, err := d.Store.ListCreativeDiscussionIDs("question", store.CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID})
	if err != nil || len(ids) != 1 {
		t.Fatalf("FYI ids %v %v", ids, err)
	}
	q, err := d.Store.GetQuestion(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != store.QuestionTypeFYI || q.Status != "resolved" || q.Resolution != store.QuestionResolutionFYI || q.ResolvedAt == 0 || q.AskedBy != "orch" || !strings.Contains(q.Body, "Correct the publication") {
		t.Fatalf("FYI %+v", q)
	}
	participants, err := d.Store.ListParticipants(q.ID)
	if err != nil || !contains(participants, store.ParticipantHuman) || !contains(participants, "orch") {
		t.Fatalf("participants %v %v", participants, err)
	}
	b, err := d.Store.GetCreativeDiscussionContext("question", q.ID)
	if err != nil || len(b.ArtifactVersionIDs) != 1 || b.ArtifactVersionIDs[0] != version {
		t.Fatalf("FYI context %+v %v", b, err)
	}
	visible := creativeRequest(t, d, "GET", fmt.Sprintf("/v1/creative/runs/%d/questions?stage_id=%d", r.ID, r.Stages[0].ID), "", "", 200)
	var notices []creativeQuestionResponse
	if err := json.Unmarshal(visible, &notices); err != nil || len(notices) != 1 || notices[0].Status != "resolved" {
		t.Fatalf("human discussion visibility %s %v", visible, err)
	}
	e := lifecycleEvent(t, d, "withdraw")
	if e.Data["reason"] != "Correct the publication" || e.Data["actor_session_id"] != "orch" || e.Data["review_revision"] != float64(1) || e.Data["notification_status"] != "recorded" {
		t.Fatalf("withdraw event %+v", e)
	}
	got, err := d.Store.GetCreativeRun(r.ID)
	if err != nil || got.Stages[0].Status != "in_progress" || got.Stages[0].ArtifactVersionIDs[0] != version {
		t.Fatalf("withdraw snapshot %+v %v", got, err)
	}
}
func TestCreativeLifecycleNotificationFailureKeepsDecision(t *testing.T) {
	for _, action := range []string{"approve", "withdraw"} {
		t.Run(action, func(t *testing.T) {
			d, r, path, _ := lifecycleSetup(t)
			db, err := sql.Open("sqlite", filepath.Join(d.Cfg.Home, "rocket.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			table := "messages"
			caller := ""
			if action == "withdraw" {
				table = "questions"
				caller = "orch"
			}
			if _, err = db.Exec(`CREATE TRIGGER fail_notification BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(FAIL,'synthetic notification failure'); END`); err != nil {
				t.Fatal(err)
			}
			creativeRequest(t, d, "POST", path, fmt.Sprintf(`{"action":%q,"review_revision":1,"reason":"Correction"}`, action), caller, 200)
			got, err := d.Store.GetCreativeRun(r.ID)
			want := "completed"
			if action == "withdraw" {
				want = "in_progress"
			}
			if err != nil || got.Stages[0].Status != want {
				t.Fatalf("committed decision %+v %v", got, err)
			}
			e := lifecycleEvent(t, d, action)
			if e.Data["notification_status"] != "failed" || !strings.Contains(fmt.Sprint(e.Data["notification_error"]), "synthetic notification failure") {
				t.Fatalf("unobservable failure %+v", e)
			}
		})
	}
}

func TestCreativeLifecycleItemWithdrawThenDecisions(t *testing.T) {
	d, r, first, _ := lifecycleSetup(t)
	creativeRequest(t, d, "POST", first, `{"action":"approve","review_revision":1}`, "", 200)
	stage := r.Stages[1].ID
	base := fmt.Sprintf("/v1/creative/runs/%d/stages/%d", r.ID, stage)
	creativeRequest(t, d, "POST", base+"/actions", `{"action":"start"}`, "orch", 200)
	item, err := d.Store.CreateCreativeItem(r.ID, stage, store.CreativeItemInput{Name: "Scene"})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s/items/%d/actions", base, item.ID)
	creativeRequest(t, d, "POST", path, `{"action":"start"}`, "orch", 200)
	publish := fmt.Sprintf("/v1/creative/runs/%d/artifacts?stage_id=%d&item_id=%d&name=Scene&kind=result&filename=scene.txt", r.ID, stage, item.ID)
	pub := decodePublication(t, artifactRequest(NewHandler(d), "POST", publish, "text/plain", "orch", strings.NewReader("scene")))
	submit := fmt.Sprintf(`{"action":"submit","artifact_version_ids":[%d]}`, pub.Version.ID)
	creativeRequest(t, d, "POST", path, submit, "orch", 200)
	creativeRequest(t, d, "POST", path, `{"action":"withdraw","review_revision":1,"reason":"Fix scene"}`, "orch", 200)
	// Binding and current-orchestrator checks must not be bypassed by same-project sessions.
	other, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: r.PipelineID, Name: "Other", Brief: "Other run"})
	if err != nil {
		t.Fatal(err)
	}
	addTestSession(t, d, "other-orch", "orchestrator", "p")
	if err = d.Store.BindCreativeSession("other-orch", store.CreativeBinding{RunID: other.ID}); err != nil {
		t.Fatal(err)
	}
	if err = d.Store.SetCreativeRunOrchestrator(other.ID, "other-orch"); err != nil {
		t.Fatal(err)
	}
	creativeRequest(t, d, "POST", path, submit, "orch", 200)
	creativeRequest(t, d, "POST", path, `{"action":"withdraw","review_revision":2,"reason":"Wrong run"}`, "other-orch", 403)
	creativeRequest(t, d, "POST", path, `{"action":"withdraw","review_revision":1,"reason":"Stale"}`, "orch", 409)
	creativeRequest(t, d, "POST", path, `{"action":"reject","review_revision":2}`, "", 200)
	creativeRequest(t, d, "POST", path, submit, "orch", 200)
	creativeRequest(t, d, "POST", path, `{"action":"approve","review_revision":3}`, "", 200)
	b := store.CreativeBinding{RunID: r.ID, StageID: stage, ItemID: item.ID}
	ids, err := d.Store.ListCreativeDiscussionIDs("message", b)
	if err != nil || len(ids) != 2 {
		t.Fatalf("item notifications %v %v", ids, err)
	}
	for _, id := range ids {
		ctx, err := d.Store.GetCreativeDiscussionContext("message", id)
		if err != nil || ctx.ItemID != item.ID || len(ctx.ArtifactVersionIDs) != 1 || ctx.ArtifactVersionIDs[0] != pub.Version.ID {
			t.Fatalf("item snapshot %+v %v", ctx, err)
		}
	}
	e := lifecycleEvent(t, d, "approve")
	if e.Type != "creative.item_action" || e.Data["review_revision"] != float64(3) || e.Data["item_id"] != float64(item.ID) {
		t.Fatalf("item event %+v", e)
	}
}
func TestCreativeLifecycleMissingOrchestratorObservableWithoutReplay(t *testing.T) {
	d, r := creativeAPISetup(t)
	path := fmt.Sprintf("/v1/creative/runs/%d/stages/%d/actions", r.ID, r.Stages[0].ID)
	for _, body := range []string{`{"action":"start"}`, `{"action":"submit"}`, `{"action":"approve","review_revision":1}`} {
		creativeRequest(t, d, "POST", path, body, "", 200)
	}
	e := lifecycleEvent(t, d, "approve")
	if e.Data["notification_status"] != "failed" || !strings.Contains(fmt.Sprint(e.Data["notification_error"]), "no current orchestrator") {
		t.Fatalf("missing orchestrator %+v", e)
	}
	if err := d.Store.BindCreativeSession("orch", store.CreativeBinding{RunID: r.ID}); err != nil {
		t.Fatal(err)
	}
	creativeRequest(t, d, "PUT", fmt.Sprintf("/v1/creative/runs/%d/orchestrator", r.ID), `{"session_id":"orch"}`, "", 200)
	creativeRequest(t, d, "GET", fmt.Sprintf("/v1/creative/runs/%d", r.ID), "", "", 200)
	ids, err := d.Store.ListCreativeDiscussionIDs("message", store.CreativeBinding{RunID: r.ID, StageID: r.Stages[0].ID})
	if err != nil || len(ids) != 0 {
		t.Fatalf("replayed history %v %v", ids, err)
	}
}
