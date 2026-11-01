package claudeinstance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func appendRecord(t *testing.T, path, kind, role, session, uuid, text string) {
	t.Helper()
	b, e := json.Marshal(map[string]any{"type": kind, "sessionId": session, "uuid": uuid, "message": map[string]any{"role": role, "content": text}})
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e = f.Write(append(b, '\n')); e != nil {
		t.Fatal(e)
	}
}
func TestExactJournalProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.jsonl")
	if e := os.WriteFile(path, nil, 0600); e != nil {
		t.Fatal(e)
	}
	id := Identity{ClaudeSessionID: "target", JournalPath: path}
	envelope := "<cross-session-message from-name=\"rocket\">\nfeedback\n</cross-session-message>"
	appendRecord(t, path, "user", "user", "target", "old", envelope)
	boundary, e := PinJournal(id)
	if e != nil || boundary.Offset <= 0 || boundary.Inode == 0 {
		t.Fatalf("pin: %+v %v", boundary, e)
	}
	s := Snapshot{Target: id, Envelope: envelope, Boundary: boundary}
	for _, tc := range []struct{ name, kind, role, uuid, session, text string }{
		{"assistant", "assistant", "assistant", "a", "target", envelope},
		{"tool", "user", "user", "b", "target", "tool output " + envelope},
		{"foreign", "user", "user", "c", "foreign", envelope},
		{"body only", "user", "user", "d", "target", "feedback"},
		{"quoted", "user", "user", "e", "target", "> " + envelope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			appendRecord(t, path, tc.kind, tc.role, tc.session, tc.uuid, tc.text)
			if p, ok, e := FindProof(context.Background(), s); ok || e != nil {
				t.Fatalf("false proof %+v %v %v", p, ok, e)
			}
		})
	}
	appendRecord(t, path, "user", "user", "target", "incoming", envelope)
	proof, ok, e := FindProof(context.Background(), s)
	if e != nil || !ok || proof.RecordID != "incoming" || proof.Offset < boundary.Offset {
		t.Fatalf("exact proof: %+v %v %v", proof, ok, e)
	}
	appendRecord(t, path, "user", "user", "target", "second", envelope)
	if _, ok, _ := FindProof(context.Background(), s); ok {
		t.Fatal("multiple matching records must remain ambiguous")
	}
}
func TestJournalReplacementAndTruncation(t *testing.T) {
	for _, mode := range []string{"replace", "truncate"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target.jsonl")
			os.WriteFile(path, []byte("old\n"), 0600)
			id := Identity{ClaudeSessionID: "target", JournalPath: path}
			b, e := PinJournal(id)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "replace" {
				os.Rename(path, path+".old")
			}
			os.WriteFile(path, nil, 0600)
			if _, ok, e := FindProof(context.Background(), Snapshot{Target: id, Boundary: b}); ok || e == nil {
				t.Fatal("changed journal accepted")
			}
		})
	}
}

func TestDocumentedCLIJournalWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.jsonl")
	os.WriteFile(path, nil, 0600)
	id := Identity{ClaudeSessionID: "target", JournalPath: path}
	b, e := PinJournal(id)
	if e != nil {
		t.Fatal(e)
	}
	env := "<cross-session-message>\nfeedback\n</cross-session-message>"
	text := "Another Claude session sent a message:\n" + env + "\n\nThis came from another Claude session — not typed by your user, but very likely working on their behalf."
	appendRecord(t, path, "user", "user", "target", "incoming", text)
	if _, ok, e := FindProof(context.Background(), Snapshot{Target: id, Boundary: b, Envelope: env}); !ok || e != nil {
		t.Fatalf("documented wrapper not proven: %v %v", ok, e)
	}
}

func TestOldPartialRecordCompletedAfterBoundaryIsNotProof(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.jsonl")
	env := "<cross-session-message>\nfeedback\n</cross-session-message>"
	raw, e := json.Marshal(map[string]any{"type": "user", "sessionId": "target", "uuid": "old-partial", "message": map[string]string{"role": "user", "content": env}})
	if e != nil {
		t.Fatal(e)
	}
	cut := len(raw) / 2
	if e = os.WriteFile(path, raw[:cut], 0600); e != nil {
		t.Fatal(e)
	}
	id := Identity{ClaudeSessionID: "target", JournalPath: path}
	if _, e = PinJournal(id); e == nil {
		t.Fatal("incomplete prewrite tail must prevent pin/send")
	}
	f, st, e := openJournal(path)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	dev, ino, e := fileID(st)
	if e != nil {
		t.Fatal(e)
	}
	f, e = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.Write(append(raw[cut:], '\n'))
	f.Close()
	s := Snapshot{Target: id, Envelope: env, Boundary: JournalBoundary{Path: path, Device: dev, Inode: ino, Offset: int64(cut)}}
	if p, ok, e := FindProof(context.Background(), s); ok || e != nil {
		t.Fatalf("completed old record became new proof: %+v %v %v", p, ok, e)
	}
}
