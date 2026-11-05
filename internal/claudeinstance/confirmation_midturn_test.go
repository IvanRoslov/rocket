package claudeinstance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The CLI structure is observed; all IDs and message content here are synthetic.
func midturnRecords(s Snapshot) (map[string]any, map[string]any) {
	removal := map[string]any{"type": "queue-operation", "operation": "remove", "reason": "absorbed_mid_turn", "sessionId": s.Target.ClaudeSessionID, "content": s.Envelope, "commandUuid": "command", "deliveryId": "delivery"}
	rendered := "<system-reminder id=\"0123456789abcdef\">\nAnother Claude session sent a message while you were working:\n" + s.Envelope + observedCLIMessageSuffix + " After completing your current task, decide whether/how to respond (reply via SendMessage to the `from=` address).\n</system-reminder id=\"0123456789abcdef\">"
	attachment := map[string]any{"type": "attachment", "sessionId": s.Target.ClaudeSessionID, "uuid": "consumed", "renderedRole": "system", "rendered": []any{map[string]any{"content": rendered}}, "attachment": map[string]any{"type": "queued_command", "prompt": s.Envelope, "source_uuid": "command", "delivery_id": "delivery", "commandMode": "prompt", "isMeta": true, "reminderId": "0123456789abcdef", "origin": map[string]any{"kind": "peer", "from": "uds:" + s.ReplyPath, "msg_id": s.WireID}}}
	return removal, attachment
}

func TestMidturnConsumptionProof(t *testing.T) {
	for _, name := range []string{"valid", "encoded reply", "missing removal", "enqueue only", "wrong reason", "foreign removal", "wrong removal text", "missing command", "wrong command", "wrong delivery", "duplicate removal", "repeated consumption", "removal after attachment", "reversed records", "foreign attachment", "missing uuid", "wrong attachment type", "wrong prompt", "wrong mode", "not meta", "not peer", "wrong wire", "missing wire", "wrong address", "missing reply", "wrong role", "missing render", "extra render", "wrong render", "wrong reminder", "missing reminder", "incomplete", "duplicate attachment", "ordinary plus attachment", "metadata only", "before boundary"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			s := Snapshot{Target: Identity{ClaudeSessionID: "session", JournalPath: path}, Envelope: "<cross-session-message msg-id=\"wire\">synthetic</cross-session-message>", WireID: "wire", ReplyPath: "/tmp/synthetic.sock"}
			s.Boundary, _ = PinJournal(s.Target)
			removal, record := midturnRecords(s)
			a := record["attachment"].(map[string]any)
			origin := a["origin"].(map[string]any)
			switch name {
			case "encoded reply":
				s.ReplyPath = "/tmp/synthetic reply%.sock"
				origin["from"] = "uds:/tmp/synthetic%20reply%25.sock"
			case "enqueue only":
				removal["operation"] = "enqueue"
			case "wrong reason":
				removal["reason"] = "cancelled"
			case "foreign removal":
				removal["sessionId"] = "foreign"
			case "wrong removal text":
				removal["content"] = "other"
			case "missing command":
				removal["commandUuid"] = ""
				a["source_uuid"] = ""
			case "wrong command":
				a["source_uuid"] = "other"
			case "wrong delivery":
				a["delivery_id"] = "other"
			case "foreign attachment":
				record["sessionId"] = "foreign"
			case "missing uuid":
				delete(record, "uuid")
			case "wrong attachment type":
				a["type"] = "hook_success"
			case "wrong prompt":
				a["prompt"] = "quoted: " + s.Envelope
			case "wrong mode":
				a["commandMode"] = "bash"
			case "not meta":
				a["isMeta"] = false
			case "not peer":
				origin["kind"] = "local"
			case "wrong wire":
				origin["msg_id"] = "other"
			case "missing wire":
				origin["msg_id"] = ""
				s.WireID = ""
			case "wrong address":
				origin["from"] = "uds:/tmp/foreign.sock"
			case "missing reply":
				s.ReplyPath = ""
				origin["from"] = "uds:"
			case "wrong role":
				record["renderedRole"] = "assistant"
			case "missing render":
				delete(record, "rendered")
			case "extra render":
				record["rendered"] = append(record["rendered"].([]any), map[string]any{"content": "extra"})
			case "wrong render":
				r := record["rendered"].([]any)[0].(map[string]any)
				r["content"] = strings.Replace(r["content"].(string), "never edit", "please edit", 1)
			case "wrong reminder":
				a["reminderId"] = "fedcba9876543210"
			case "missing reminder":
				delete(a, "reminderId")
			}
			var data []byte
			appendJSON := func(v any) {
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, append(b, '\n')...)
			}
			if name == "reversed records" {
				appendJSON(record)
			}
			if name != "missing removal" {
				appendJSON(removal)
			}
			if name == "duplicate removal" {
				appendJSON(removal)
			}
			start := int64(len(data))
			if name == "metadata only" {
				record["type"] = "api-request-blob"
			}
			if name != "reversed records" {
				appendJSON(record)
			}
			end := int64(len(data))
			if name == "repeated consumption" || name == "removal after attachment" {
				appendJSON(removal)
			}
			if name == "duplicate attachment" || name == "repeated consumption" {
				record["uuid"] = "duplicate"
				appendJSON(record)
			}
			if name == "ordinary plus attachment" {
				appendJSON(map[string]any{"type": "user", "sessionId": "session", "uuid": "ordinary", "message": map[string]any{"role": "user", "content": s.Envelope}})
			}
			if name == "incomplete" {
				data = data[:len(data)-1]
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if name == "before boundary" {
				s.Boundary, _ = PinJournal(s.Target)
			}
			p, found, err := FindProof(context.Background(), s)
			if err != nil || found != (name == "valid" || name == "encoded reply") {
				t.Fatalf("found=%v proof=%+v err=%v", found, p, err)
			}
			if found && p != (Proof{RecordID: "consumed", Offset: start, End: end}) {
				t.Fatalf("wrong physical proof: %+v", p)
			}
		})
	}
}
