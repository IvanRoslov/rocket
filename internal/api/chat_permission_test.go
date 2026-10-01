package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// transcriptTS is the unix time of writeChatTranscript's i-th line.
func transcriptTS(i int) int64 {
	return time.Date(2026, 7, 18, 21, 0, i, 0, time.UTC).Unix()
}

type chatBody struct {
	Entries []chatEntryResponse `json:"entries"`
	Next    string              `json:"next_cursor"`
}

func getChat(t *testing.T, base, query string) chatBody {
	t.Helper()
	resp := getJSON(t, base+"/v1/sessions/sess1/chat"+query)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET chat%s: status %d", query, resp.StatusCode)
	}
	var b chatBody
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return b
}

// resolvedPermission journals a dialog asked at askedAt and closes it,
// answered from chat with label when label is set.
func resolvedPermission(t *testing.T, st *store.Store, title string, askedAt int64, label string) int64 {
	t.Helper()
	id, _, err := st.OpenPermissionPrompt("sess1", title, "ctx "+title, `["Yes","No"]`, askedAt)
	if err != nil {
		t.Fatal(err)
	}
	if label != "" {
		_ = st.MarkPermissionPromptAnswered(id, label)
	}
	if err := st.ResolvePermissionPrompt(id, askedAt+5); err != nil {
		t.Fatal(err)
	}
	return id
}

func entryTexts(es []chatEntryResponse) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Role+":"+e.Text)
	}
	return strings.Join(out, " | ")
}

func countPermission(es []chatEntryResponse) int {
	n := 0
	for _, e := range es {
		if e.Role == "permission" {
			n++
		}
	}
	return n
}

func permissionChatSetup(t *testing.T, lines int) (Deps, string, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"
	writeChatTranscript(t, base, wt, lines)
	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)
	return d, srv.URL, filepath.Join(base, "projects", chatSlugify(wt), "sess.jsonl")
}

func TestChatTailMergesResolvedPermissionsByTS(t *testing.T) {
	d, base, _ := permissionChatSetup(t, 3)
	resolvedPermission(t, d.Store, "Edit?", transcriptTS(1), "Yes")
	if _, _, err := d.Store.OpenPermissionPrompt("sess1", "Open?", "", "[]", transcriptTS(2)); err != nil {
		t.Fatal(err)
	}

	b := getChat(t, base, "")
	if got := entryTexts(b.Entries); got != "user:msg 0 | user:msg 1 | permission:Edit? | user:msg 2" {
		t.Fatalf("entries = %s", got)
	}
	p := b.Entries[2]
	if p.TS != transcriptTS(1) || p.Permission == nil {
		t.Fatalf("permission entry = %+v", p)
	}
	want := permissionEntryResponse{Title: "Edit?", Context: "ctx Edit?", AnswerLabel: "Yes", AnsweredVia: "chat"}
	if *p.Permission != want {
		t.Errorf("permission = %+v, want %+v", *p.Permission, want)
	}
	if !strings.Contains(b.Next, "#p") {
		t.Errorf("next_cursor = %q, want a permission watermark", b.Next)
	}

	// The JSON shape clients read.
	raw, _ := json.Marshal(p)
	if !strings.Contains(string(raw), `"permission":{"title":"Edit?","context":"ctx Edit?","answer_label":"Yes","answered_via":"chat"}`) {
		t.Errorf("entry JSON = %s", raw)
	}
}

func TestChatTailWithoutPermissionsKeepsPlainCursor(t *testing.T) {
	_, base, _ := permissionChatSetup(t, 2)
	b := getChat(t, base, "")
	if strings.Contains(b.Next, "#p") {
		t.Errorf("next_cursor = %q, want no watermark for a session without permission rows", b.Next)
	}
}

// TestChatIncrementalDeliversEachPermissionOnce: a row is never re-sent on
// later cursor reads (web treats a repeated first entry as a cursor
// rollback and would replace its whole feed), and a row resolved after a
// read arrives exactly once on the next one.
func TestChatIncrementalDeliversEachPermissionOnce(t *testing.T) {
	d, base, path := permissionChatSetup(t, 3)
	resolvedPermission(t, d.Store, "First?", transcriptTS(1), "")

	b1 := getChat(t, base, "")
	if countPermission(b1.Entries) != 1 {
		t.Fatalf("tail entries = %s", entryTexts(b1.Entries))
	}

	b2 := getChat(t, base, "?cursor="+url.QueryEscape(b1.Next))
	if len(b2.Entries) != 0 {
		t.Fatalf("idle cursor read = %s, want nothing", entryTexts(b2.Entries))
	}

	resolvedPermission(t, d.Store, "Second?", transcriptTS(3), "No")
	appendChatLine(t, path, `{"type":"user","timestamp":"2026-07-18T21:00:04Z","message":{"role":"user","content":"msg 4"}}`)

	b3 := getChat(t, base, "?cursor="+url.QueryEscape(b2.Next))
	if got := entryTexts(b3.Entries); got != "permission:Second? | user:msg 4" {
		t.Fatalf("cursor read = %s", got)
	}

	b4 := getChat(t, base, "?cursor="+url.QueryEscape(b3.Next))
	if len(b4.Entries) != 0 {
		t.Fatalf("follow-up cursor read = %s, want nothing", entryTexts(b4.Entries))
	}
}

// TestChatFallbackCursorReDeliversPermissionsInPlace: when the transcript
// cursor is stale the adapter re-reads from scratch; that batch must carry
// the permission rows in place, exactly once each, like a tail read — the
// clients locate their own tail inside it, permission entries included.
func TestChatFallbackCursorReDeliversPermissionsInPlace(t *testing.T) {
	d, base, _ := permissionChatSetup(t, 3)
	resolvedPermission(t, d.Store, "Edit?", transcriptTS(1), "Yes")

	b1 := getChat(t, base, "")
	_, suffix, found := strings.Cut(b1.Next, "#p")
	if !found {
		t.Fatalf("next_cursor = %q, want a watermark", b1.Next)
	}
	stale := "/tmp/does/not/exist.jsonl:0#p" + suffix

	b2 := getChat(t, base, "?cursor="+url.QueryEscape(stale))
	if got := entryTexts(b2.Entries); got != "user:msg 0 | user:msg 1 | permission:Edit? | user:msg 2" {
		t.Fatalf("fallback read = %s", got)
	}
	e1, _ := json.Marshal(b1.Entries[2])
	e2, _ := json.Marshal(b2.Entries[2])
	if string(e1) != string(e2) {
		t.Errorf("re-delivered permission entry %+v differs from %+v (clients dedupe by ts/role/text)", b2.Entries[2], b1.Entries[2])
	}
}

// TestChatCursorFromBeforeUpgradeSkipsHistory: a client that loaded the
// feed before permission rows existed holds a cursor without a watermark;
// it gets no historic rows dumped at the end of its feed.
func TestChatCursorFromBeforeUpgradeSkipsHistory(t *testing.T) {
	d, base, _ := permissionChatSetup(t, 3)
	b1 := getChat(t, base, "")
	resolvedPermission(t, d.Store, "Old?", transcriptTS(1), "")

	b2 := getChat(t, base, "?cursor="+url.QueryEscape(b1.Next))
	if countPermission(b2.Entries) != 0 {
		t.Fatalf("cursor read = %s, want no historic permission rows", entryTexts(b2.Entries))
	}
	if !strings.Contains(b2.Next, "#p") {
		t.Errorf("next_cursor = %q, want the watermark from now on", b2.Next)
	}
}

func TestChatTailLimitCountsPermissionEntries(t *testing.T) {
	d, base, _ := permissionChatSetup(t, 5)
	resolvedPermission(t, d.Store, "Late?", transcriptTS(3), "")

	b := getChat(t, base, "?limit=3")
	if got := entryTexts(b.Entries); got != "user:msg 3 | permission:Late? | user:msg 4" {
		t.Fatalf("entries = %s", got)
	}
}
