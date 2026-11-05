package api

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/IvanRoslov/rocket/internal/store"
)

// notifyCreativeAction is deliberately post-commit: delivery availability must
// never change a committed review decision. Its outcome joins the existing action
// event. There is no replay: existing queued rows own subsequent delivery state.
func notifyCreativeAction(d Deps, run store.CreativeRun, b store.CreativeBinding, action store.CreativeAction, data map[string]any) {
	if action.Action != "approve" && action.Action != "reject" && action.Action != "withdraw" {
		return
	}
	payload, _ := json.Marshal(data)
	body := fmt.Sprintf("%s\nCreative review decision committed:\n%s", creativeContextPrefix(b), payload)
	var err error
	if action.Action == "withdraw" {
		body += "\nReview withdrawn; work is back in progress. Reason: " + action.Reason
		var id int64
		actor, _ := data["actor_session_id"].(string)
		id, err = d.Store.AddCreativeQuestion(store.Question{AskedBy: actor, Title: "Review withdrawn", Body: body, Type: store.QuestionTypeFYI}, b)
		if err == nil {
			data["notification_question_id"] = id
			err = d.Store.AddParticipants(id, store.ParticipantHuman, actor)
		}
		if err == nil {
			data["notification_status"] = "recorded"
			if d.Bus != nil {
				d.Bus.Publish("creative.question_changed", "", map[string]any{"run_id": b.RunID, "question_id": id})
			}
		}
	} else {
		if action.Action == "approve" {
			body += "\nVerify that the completed assignment outputs are durably published, then explicitly close the appropriate stage/item workers with rocket creative agent close RUN WORKER_SESSION. Approval does not close workers automatically."
		}
		to := run.OrchestratorSessionID
		data["notification_to"] = to
		if to == "" {
			err = fmt.Errorf("run has no current orchestrator")
		} else {
			var bound store.CreativeBinding
			bound, err = d.Store.GetCreativeSessionContext(to)
			if err == nil && bound.RunID != run.ID {
				err = fmt.Errorf("current orchestrator is bound to another run")
			}
			if err == nil {
				var id int64
				// Empty sender means system/unattributed, consistent with the action event;
				// it must not impersonate an authenticated individual.
				id, err = d.Store.AddCreativeMessage(store.Message{ToSession: to, Body: body}, b)
				if err == nil {
					data["notification_status"], data["notification_message_id"] = "queued", id
					wakeCreativeMessage(d, id, to)
				}
			}
		}
	}
	if err != nil {
		data["notification_status"], data["notification_error"] = "failed", err.Error()
		slog.Error("creative action notification failed after commit", "run_id", b.RunID, "stage_id", b.StageID, "item_id", b.ItemID, "action", action.Action, "review_revision", action.ReviewRevision, "error", err)
	}
}
