package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
)

const journalTestUUID = "329baa3b-27eb-40ed-82e1-1d9f3dde01e5"

// Sanitized metadata types from the acceptance evidence. No live transcript
// content is used.
const claudeJournalMetadata = `{"type":"last-prompt","sessionId":"SESSION","lastPrompt":"fixture"}
{"type":"mode","sessionId":"SESSION","mode":"default"}
{"type":"permission-mode","sessionId":"SESSION","permissionMode":"default"}
{"type":"atis-latch","sessionId":"SESSION"}
{"type":"ai-title","sessionId":"SESSION","title":"fixture"}
{"type":"tools-used","sessionId":"SESSION","tools":[]}
{"type":"api-request-blob","sessionId":"SESSION","timestamp":null}
{"type":"attachment","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"api-request-shape","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"api-request","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"system","subtype":"stop_hook_summary","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"system","subtype":"turn_duration","sessionId":"SESSION","timestamp":"STAMP"}
`

// Representative real ordering: startup metadata, incoming user, API request
// metadata, title/tools metadata, assistant, then end-of-turn metadata.
const claudeJournalOrderedFixture = `{"type":"last-prompt","sessionId":"SESSION","lastPrompt":"fixture"}
{"type":"mode","sessionId":"SESSION","mode":"default"}
{"type":"permission-mode","sessionId":"SESSION","permissionMode":"default"}
{"type":"atis-latch","sessionId":"SESSION"}
{"type":"attachment","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"user","sessionId":"SESSION","uuid":"user-1","timestamp":"STAMP","message":{"role":"user","content":"fixture prompt"}}
{"type":"attachment","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"api-request-shape","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"api-request-blob","sessionId":"SESSION"}
{"type":"api-request-blob","sessionId":"SESSION"}
{"type":"api-request","sessionId":"SESSION","timestamp":"STAMP"}
{"type":"last-prompt","sessionId":"SESSION","lastPrompt":"fixture"}
{"type":"mode","sessionId":"SESSION","mode":"default"}
{"type":"permission-mode","sessionId":"SESSION","permissionMode":"default"}
{"type":"atis-latch","sessionId":"SESSION"}
{"type":"ai-title","sessionId":"SESSION","title":"fixture"}
{"type":"tools-used","sessionId":"SESSION","tools":[]}
{"type":"assistant","sessionId":"SESSION","uuid":"assistant-1","timestamp":"STAMP","message":{"role":"assistant","content":[{"type":"text","text":"fixture reply"}]}}
{"type":"assistant","sessionId":"SESSION","uuid":"assistant-2","timestamp":"STAMP","isApiErrorMessage":ERRORFLAG,"message":{"role":"assistant","content":[{"type":"text","text":"fixture reply"}]}}
{"type":"attachment","sessionId":"SESSION","timestamp":"METASTAMP"}
{"type":"system","subtype":"stop_hook_summary","sessionId":"SESSION","timestamp":"METASTAMP"}
{"type":"system","subtype":"turn_duration","sessionId":"SESSION","timestamp":"METASTAMP"}
`

func journalFixtureText(raw string, at time.Time) string {
	return strings.NewReplacer("SESSION", journalTestUUID, "STAMP", at.Format(time.RFC3339Nano)).Replace(raw)
}

func journalFixtureIdentity(t *testing.T, raw string) claudeinstance.Identity {
	t.Helper()
	id := claudeinstance.Identity{
		ClaudeSessionID: journalTestUUID,
		ProcessStart:    time.Now().Add(-5 * time.Minute),
		JournalPath:     filepath.Join(t.TempDir(), journalTestUUID+".jsonl"),
	}
	if err := os.WriteFile(id.JournalPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestClaudeJournalMetadataOrdering(t *testing.T) {
	for _, want := range []activity.State{activity.Ready, activity.Active, activity.Blocked} {
		t.Run(string(want), func(t *testing.T) {
			at := time.Now().Add(-time.Second).UTC()
			if want != activity.Active {
				at = at.Add(-time.Minute)
			}
			// Metadata timestamps must not replace the actual lifecycle timestamp.
			flag := "false"
			if want == activity.Blocked {
				flag = "true"
			}
			raw := strings.NewReplacer("METASTAMP", time.Now().Add(-time.Millisecond).Format(time.RFC3339Nano), "ERRORFLAG", flag).Replace(claudeJournalOrderedFixture)
			raw = journalFixtureText(raw, at)
			m, st, _, _ := claudeMonitorFixture(t)
			m.cfg.ReadyToIdle = time.Hour
			_, id, err := st.GetClaudeLaunch("s")
			if err != nil || id == nil {
				t.Fatalf("fixture identity: %v %v", id, err)
			}
			if err := os.WriteFile(id.JournalPath, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			got, ts, err := claudeJournalActivity(context.Background(), *id)
			if err != nil || got != want || !ts.Equal(at) {
				t.Fatalf("ordered journal: state=%q timestamp=%v err=%v; want %q at %v", got, ts, err, want, at)
			}
			sess, err := st.GetSession("s")
			if err != nil {
				t.Fatal(err)
			}
			m.pollSession(context.Background(), sess, map[string]bool{"s": true}, true)
			sess, err = st.GetSession("s")
			if err != nil || sess.Activity != string(want) || sess.ActivityTS != at.Unix() {
				t.Fatalf("poll did not persist lifecycle: state=%q timestamp=%d err=%v", sess.Activity, sess.ActivityTS, err)
			}
		})
	}
}

func TestClaudeJournalMetadataCannotEstablishActivity(t *testing.T) {
	for _, line := range strings.Split(strings.TrimSpace(claudeJournalMetadata), "\n") {
		t.Run(line, func(t *testing.T) {
			id := journalFixtureIdentity(t, journalFixtureText(line+"\n", time.Now().Add(-time.Second)))
			state, ts, err := claudeJournalActivity(context.Background(), id)
			if !errors.Is(err, agent.ErrNoSignal) || state != "" || !ts.IsZero() {
				t.Fatalf("metadata established activity: %q %v %v", state, ts, err)
			}
		})
	}
}

func TestClaudeJournalBlockedSurvivesMetadata(t *testing.T) {
	for _, flag := range []string{`"isApiErrorMessage":true,`, `"message":{"isApiErrorMessage":true},`} {
		t.Run(flag, func(t *testing.T) {
			at := time.Now().Add(-time.Minute).UTC()
			raw := journalFixtureText(`{"type":"assistant","sessionId":"SESSION",`+flag+`"timestamp":"STAMP"}`+"\n", at) +
				journalFixtureText(claudeJournalMetadata, time.Now().Add(-time.Second))
			id := journalFixtureIdentity(t, raw)
			state, ts, err := claudeJournalActivity(context.Background(), id)
			if err != nil || state != activity.Blocked || !ts.Equal(at) {
				t.Fatalf("metadata cleared block: %q %v %v", state, ts, err)
			}
		})
	}
}

func TestClaudeJournalConservativeRecords(t *testing.T) {
	at := time.Now().Add(-time.Minute).UTC()
	valid := journalFixtureText(`{"type":"assistant","sessionId":"SESSION","timestamp":"STAMP"}`+"\n", at)
	for _, tc := range []struct{ name, tail string }{
		{"missing user timestamp", `{"type":"user","sessionId":"SESSION"}` + "\n"},
		{"missing assistant timestamp", `{"type":"assistant","sessionId":"SESSION"}` + "\n"},
		{"null timestamp", `{"type":"assistant","sessionId":"SESSION","timestamp":null}` + "\n"},
		{"invalid timestamp", `{"type":"assistant","sessionId":"SESSION","timestamp":"invalid"}` + "\n"},
		{"zero timestamp", `{"type":"assistant","sessionId":"SESSION","timestamp":"0001-01-01T00:00:00Z"}` + "\n"},
		{"before root", `{"type":"user","sessionId":"SESSION","timestamp":"2000-01-01T00:00:00Z"}` + "\n"},
		{"future timestamp", `{"type":"user","sessionId":"SESSION","timestamp":"2999-01-01T00:00:00Z"}` + "\n"},
		{"unknown missing timestamp", `{"type":"unknown","sessionId":"SESSION"}` + "\n"},
		{"meaningful system missing timestamp", `{"type":"system","subtype":"api_error","sessionId":"SESSION"}` + "\n"},
		{"metadata error missing timestamp", `{"type":"mode","sessionId":"SESSION","isApiErrorMessage":true}` + "\n"},
		{"metadata invalid timestamp", `{"type":"mode","sessionId":"SESSION","timestamp":"invalid"}` + "\n"},
		{"malformed metadata", `{"type":"last-prompt","sessionId":"SESSION",` + "\n"},
		{"malformed lifecycle", `{"type":"assistant","sessionId":"SESSION","timestamp":` + "\n"},
		{"partial metadata", `{"type":"mode","sessionId":"SESSION"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := journalFixtureIdentity(t, valid+journalFixtureText(tc.tail, at))
			state, ts, err := claudeJournalActivity(context.Background(), id)
			if !errors.Is(err, agent.ErrNoSignal) || state != "" || !ts.IsZero() {
				t.Fatalf("uncertainty became activity: %q %v %v", state, ts, err)
			}
		})
	}
}

func TestClaudeJournalMetadataIdentityAndErrorPrecedence(t *testing.T) {
	at := time.Now().Add(-time.Minute).UTC()
	for _, tc := range []struct {
		name, raw string
		want      activity.State
		noSignal  bool
	}{
		{"foreign lifecycle only", `{"type":"assistant","sessionId":"foreign","timestamp":"STAMP"}` + "\n", "", true},
		{"metadata and foreign lifecycle", claudeJournalMetadata + `{"type":"assistant","sessionId":"foreign","timestamp":"STAMP"}` + "\n", "", true},
		{"foreign error after valid", `{"type":"assistant","sessionId":"SESSION","timestamp":"STAMP"}` + "\n" + `{"type":"assistant","sessionId":"foreign","isApiErrorMessage":true}` + "\n", activity.Ready, false},
		{"top level error on metadata", `{"type":"mode","sessionId":"SESSION","timestamp":"STAMP","isApiErrorMessage":true}` + "\n", activity.Blocked, false},
		{"nested error on metadata", `{"type":"mode","sessionId":"SESSION","timestamp":"STAMP","message":{"isApiErrorMessage":true}}` + "\n", activity.Blocked, false},
		{"genuine lifecycle clears block", `{"type":"assistant","sessionId":"SESSION","timestamp":"STAMP","isApiErrorMessage":true}` + "\n" + claudeJournalMetadata + `{"type":"user","sessionId":"SESSION","timestamp":"STAMP"}` + "\n", activity.Ready, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := journalFixtureIdentity(t, journalFixtureText(tc.raw, at))
			state, ts, err := claudeJournalActivity(context.Background(), id)
			if tc.noSignal {
				if !errors.Is(err, agent.ErrNoSignal) || state != "" || !ts.IsZero() {
					t.Fatalf("wrong identity became authority: %q %v %v", state, ts, err)
				}
			} else if err != nil || state != tc.want || !ts.Equal(at) {
				t.Fatalf("state=%q at=%v err=%v; want %q at %v", state, ts, err, tc.want, at)
			}
		})
	}
}

func TestClaudeJournalMetadataGuards(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	id := journalFixtureIdentity(t, journalFixtureText(claudeJournalMetadata, at))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := claudeJournalActivity(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	// A record that fills the entire bounded tail cannot become lifecycle evidence.
	id = journalFixtureIdentity(t, `{"type":"mode","sessionId":"`+journalTestUUID+`","data":"`+strings.Repeat("x", 1<<20)+`"}`+"\n")
	if state, _, err := claudeJournalActivity(context.Background(), id); err == nil || state != "" {
		t.Fatalf("oversize metadata became authority: %q %v", state, err)
	}
}

func TestClaudeJournalMetadataProofCompatibility(t *testing.T) {
	id := journalFixtureIdentity(t, "")
	boundary, err := claudeinstance.PinJournal(id)
	if err != nil {
		t.Fatal(err)
	}
	const envelope = "<cross-session-message msg-id=\"5e248011-1d03-4fe1-a591-1bbe013451af\">\nfixture\n</cross-session-message>"
	incoming, err := json.Marshal(map[string]any{
		"type": "user", "sessionId": journalTestUUID, "uuid": "incoming-fixture", "timestamp": time.Now().Add(-time.Second),
		"message": map[string]any{"role": "user", "content": envelope},
	})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Second)
	prefix := strings.NewReplacer("METASTAMP", at.Format(time.RFC3339Nano), "ERRORFLAG", "false").Replace(claudeJournalOrderedFixture)
	prefix = journalFixtureText(prefix, at)
	metadata := journalFixtureText(claudeJournalMetadata, at)
	if err := os.WriteFile(id.JournalPath, []byte(prefix+string(incoming)+"\n"+metadata), 0600); err != nil {
		t.Fatal(err)
	}
	proof, ok, err := claudeinstance.FindProof(context.Background(), claudeinstance.Snapshot{Target: id, Boundary: boundary, Envelope: envelope})
	if err != nil || !ok || proof.RecordID != "incoming-fixture" || proof.Offset != int64(len(prefix)) || proof.End != int64(len(prefix)+len(incoming)+1) {
		t.Fatalf("metadata obstructed exact incoming proof: %+v %v %v", proof, ok, err)
	}
}
