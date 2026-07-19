package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// chatTestDeps builds Deps with a real store+bus for tests exercising
// GET /v1/sessions/{id}/chat. The claudecode package is imported (above) so
// its init() registers the "claude-code" agent builder in the global
// registry that agent.Get consults.
func chatTestDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir}

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	return d
}

// seedChatSession inserts repo/project/session rows so GetSession(id)
// succeeds, with Agent set to "claude-code" and WorktreePath pointing at wt.
func seedChatSession(t *testing.T, st *store.Store, id, wt string) {
	t.Helper()
	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "proj1", Name: "proj1", MainRepo: "repo1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := st.AddSession(store.Session{
		ID:           id,
		Kind:         "worker",
		ProjectID:    "proj1",
		RepoID:       "repo1",
		FeatureSlug:  "feat1",
		Branch:       "feature/feat1/task1",
		Agent:        "claude-code",
		WorktreePath: wt,
		TmuxName:     id,
		State:        "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

// writeChatTranscript writes a claude-code transcript with n simple user
// turns ("msg 0".."msg n-1") into base's CLAUDE_CONFIG_DIR layout for wt.
func writeChatTranscript(t *testing.T, base, wt string, n int) {
	t.Helper()
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf(`{"type":"user","timestamp":"2026-07-18T21:00:%02dZ","message":{"role":"user","content":"msg %d"}}`, i%60, i)
	}
	writeChatFile(t, base, wt, "sess.jsonl", lines)
}

// writeChatFile writes a transcript file with the given raw JSONL lines,
// following claudecode's directory layout ("<base>/projects/<slug>/<name>",
// see chatSlugify).
func writeChatFile(t *testing.T, base, wt, name string, lines []string) {
	t.Helper()
	dir := filepath.Join(base, "projects", chatSlugify(wt))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	path := filepath.Join(dir, name)
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func TestChatTailFirstCall(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeChatTranscript(t, base, wt, 12)

	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/sessions/sess1/chat?limit=5")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Entries []chatEntryResponse `json:"entries"`
		Next    string              `json:"next_cursor"`
		Session sessionRef          `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 5 {
		t.Fatalf("len(entries) = %d, want 5", len(body.Entries))
	}
	if body.Entries[0].Text != "msg 7" || body.Entries[4].Text != "msg 11" {
		t.Errorf("entries = %+v, want tail msg 7..msg 11", body.Entries)
	}
	if body.Next == "" {
		t.Errorf("next_cursor is empty, want non-empty")
	}
	if body.Session.ID != "sess1" || body.Session.Kind != "worker" || body.Session.State != "running" {
		t.Errorf("session ref = %+v", body.Session)
	}
}

func TestChatIncrementalFromCursor(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeChatTranscript(t, base, wt, 3)

	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)

	resp1 := getJSON(t, srv.URL+"/v1/sessions/sess1/chat")
	defer resp1.Body.Close()
	var body1 struct {
		Entries []chatEntryResponse `json:"entries"`
		Next    string              `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&body1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body1.Entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(body1.Entries))
	}

	// Append two more lines directly to the transcript file.
	path := filepath.Join(base, "projects", chatSlugify(wt), "sess.jsonl")
	appendChatLine(t, path, `{"type":"user","message":{"role":"user","content":"msg 3"}}`)
	appendChatLine(t, path, `{"type":"user","message":{"role":"user","content":"msg 4"}}`)

	resp2 := getJSON(t, srv.URL+"/v1/sessions/sess1/chat?cursor="+url.QueryEscape(body1.Next))
	defer resp2.Body.Close()
	var body2 struct {
		Entries []chatEntryResponse `json:"entries"`
		Next    string              `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body2.Entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (only the new lines): %+v", len(body2.Entries), body2.Entries)
	}
	if body2.Entries[0].Text != "msg 3" || body2.Entries[1].Text != "msg 4" {
		t.Errorf("entries = %+v", body2.Entries)
	}
}

func TestChatSessionNotFound(t *testing.T) {
	d := chatTestDeps(t)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/sessions/nope/chat")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestChatNoTranscriptReturnsEmptyOK(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/nowhere/worktree"

	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/sessions/sess1/chat")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Entries []chatEntryResponse `json:"entries"`
		Next    string              `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 0 {
		t.Errorf("entries = %+v, want none", body.Entries)
	}
	if body.Next != "" {
		t.Errorf("next_cursor = %q, want empty", body.Next)
	}
}

func TestChatLimitClamp(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeChatTranscript(t, base, wt, 5)

	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)

	resp := getJSON(t, srv.URL+"/v1/sessions/sess1/chat?limit=999999")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Entries []chatEntryResponse `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only 5 entries exist total, so clamping to 1000 shouldn't error or
	// misbehave; this mainly asserts the request succeeds and returns what
	// exists.
	if len(body.Entries) != 5 {
		t.Fatalf("len(entries) = %d, want 5", len(body.Entries))
	}
}

// TestChatIncrementalInvalidCursorRespectsLimit covers finding 3 of the
// final-review fix brief: an incremental request (cursor != "") whose
// cursor is invalid (here: doesn't resolve to any real file, so the
// claudecode adapter falls back to reading the newest transcript from byte
// 0, i.e. the whole thing) must still be capped at `limit` entries by the
// handler, not return the full transcript unsliced.
func TestChatIncrementalInvalidCursorRespectsLimit(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeChatTranscript(t, base, wt, 10)

	d := chatTestDeps(t)
	seedChatSession(t, d.Store, "sess1", wt)
	srv := newTestServer(t, d)

	badCursor := "/tmp/does/not/exist.jsonl:0"
	resp := getJSON(t, srv.URL+"/v1/sessions/sess1/chat?limit=3&cursor="+url.QueryEscape(badCursor))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Entries []chatEntryResponse `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3 (capped at limit despite invalid cursor forcing a from-scratch read)", len(body.Entries))
	}
	if body.Entries[0].Text != "msg 7" || body.Entries[2].Text != "msg 9" {
		t.Errorf("entries = %+v, want tail msg 7..msg 9", body.Entries)
	}
}

func appendChatLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}

// chatSlugify mirrors claudecode's unexported slugify: every '/' and '.' in
// the worktree path is replaced with '-', one-for-one. Used only to locate
// the transcript file this test itself wrote.
func chatSlugify(worktreePath string) string {
	replacer := strings.NewReplacer("/", "-", ".", "-")
	return replacer.Replace(worktreePath)
}

var _ = claudecode.New // keep the import (and its init() registration) live
