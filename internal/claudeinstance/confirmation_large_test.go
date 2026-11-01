package claudeinstance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reviewer counterexample (task5916): an unrelated large record after the
// boundary (e.g. a Read tool_result while the inbound message is queued in
// Claude's inbox) permanently prevents the exact proof from being found.
func TestReview5916LargeUnrelatedRecordBlocksProof(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.jsonl")
	os.WriteFile(path, nil, 0600)
	uuid := "329baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	id := Identity{ClaudeSessionID: uuid, JournalPath: path}
	b, err := PinJournal(id)
	if err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Target: id, Boundary: b, Envelope: "<cross-session-message>\nhello\n</cross-session-message>", PreparedAt: time.Now()}
	big, _ := json.Marshal(map[string]any{"type": "user", "sessionId": uuid, "uuid": "tool", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": strings.Repeat("x", 70*1024)}}}})
	exact, _ := json.Marshal(map[string]any{"type": "user", "sessionId": uuid, "uuid": "rec", "message": map[string]any{"role": "user", "content": s.Envelope}})
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write(append(big, '\n'))
	f.Write(append(exact, '\n'))
	f.Close()
	p, found, err := FindProof(context.Background(), s)
	t.Logf("found=%v err=%v proof=%+v", found, err, p)
	if !found {
		t.Errorf("exact unique envelope after boundary not proven because of an unrelated >64KiB record")
	}
}

func TestClaudeProofLargeCompleteRecordsRemainExactAndBounded(t *testing.T) {
	for _, which := range []string{"unrelated tool", "large exact", "escaped and metadata", "later duplicate", "incomplete exact", "total limit", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.jsonl")
			prefix := []byte("{\"type\":\"assistant\"}\n")
			if e := os.WriteFile(path, prefix, 0600); e != nil {
				t.Fatal(e)
			}
			id := Identity{ClaudeSessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", JournalPath: path}
			b, e := PinJournal(id)
			if e != nil {
				t.Fatal(e)
			}
			envelope := "<cross-session-message>feedback</cross-session-message>"
			if which == "large exact" {
				envelope = strings.Repeat("feedback", 20*1024)
			}
			if which == "escaped and metadata" {
				envelope = strings.Repeat("<>&\n", 12000)
			}
			s := Snapshot{Target: id, Boundary: b, Envelope: envelope}
			marshal := func(v any) []byte {
				r, e := json.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				return append(r, '\n')
			}
			big := marshal(map[string]any{"type": "user", "sessionId": id.ClaudeSessionID, "uuid": "tool", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": strings.Repeat("x", 70*1024)}}}})
			record := func(key string) []byte {
				v := map[string]any{"type": "user", "sessionId": id.ClaudeSessionID, "uuid": key, "message": map[string]string{"role": "user", "content": envelope}}
				if which == "escaped and metadata" {
					v["metadata"] = strings.Repeat("m", 1<<20)
				}
				return marshal(v)
			}
			exact := record("exact")
			if which == "escaped and metadata" && (len(envelope) >= 64*1024 || len(exact) <= 1<<20) {
				t.Fatal("escaping/metadata size fixture invalid")
			}
			tail := append(append([]byte(nil), big...), exact...)
			want := true
			wantErr := false
			switch which {
			case "later duplicate":
				tail = append(tail, big...)
				tail = append(tail, record("duplicate")...)
				want = false
			case "incomplete exact":
				tail = tail[:len(tail)-1]
				want = false
			case "total limit":
				tail = append(tail, []byte(strings.Repeat("x", 32<<20))...)
				want = false
				wantErr = true
			}
			f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.Write(tail); e != nil {
				f.Close()
				t.Fatal(e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			if which == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = false
				wantErr = true
			}
			proof, found, e := FindProof(ctx, s)
			if found != want || (e != nil) != wantErr {
				t.Fatalf("found=%v want=%v error=%v wantErr=%v", found, want, e, wantErr)
			}
			if found && (proof.RecordID != "exact" || proof.Offset != int64(len(prefix)+len(big)) || proof.End != int64(len(prefix)+len(big)+len(exact))) {
				t.Fatalf("incorrect complete record offsets: %+v", proof)
			}
		})
	}
}
