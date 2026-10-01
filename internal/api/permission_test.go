package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
)

func permissionPane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// seedPermissionPending puts sess1 on fixture's dialog the way the monitor
// does: journal row + pending permission quiz, and the pane showing it.
func seedPermissionPending(t *testing.T, d Deps, rt *quizAnswerFakeRuntime, fixture string) runtime.PermissionPrompt {
	t.Helper()
	seedQuizSession(t, d.Store, "sess1")
	pane := permissionPane(t, fixture)
	p, ok := runtime.ParsePermissionPrompt(pane)
	if !ok {
		t.Fatalf("fixture %s not parsed", fixture)
	}
	id, _, err := d.Store.OpenPermissionPrompt("sess1", p.Title, p.Context, "[]", 77)
	if err != nil {
		t.Fatalf("OpenPermissionPrompt: %v", err)
	}
	b, _ := json.Marshal(session.NewPermissionQuiz(p, id, 77))
	if err := d.Store.SetPendingQuiz("sess1", string(b)); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
	rt.mu.Lock()
	rt.pane = pane
	rt.mu.Unlock()
	return p
}

func TestGetSession_PermissionQuizShape(t *testing.T) {
	d, rt := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	p := seedPermissionPending(t, d, rt, "permission-edit-settings.pane")

	for _, path := range []string{"/v1/sessions/sess1", "/v1/sessions/sess1/chat"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp.Body.Close()
		if path != "/v1/sessions/sess1" {
			var sess map[string]json.RawMessage
			_ = json.Unmarshal(body["session"], &sess)
			body = sess
		}

		var pq map[string]any
		if err := json.Unmarshal(body["pending_quiz"], &pq); err != nil {
			t.Fatalf("%s: pending_quiz %s: %v", path, body["pending_quiz"], err)
		}
		if pq["source"] != "permission" || pq["raw"] != p.Raw || pq["asked_at"] != float64(77) {
			t.Errorf("%s: source/raw/asked_at = %v / %.40q / %v", path, pq["source"], pq["raw"], pq["asked_at"])
		}
		if _, leaked := pq["permission"]; leaked {
			t.Errorf("%s: internal permission ref exposed: %v", path, pq["permission"])
		}
		qs := pq["questions"].([]any)
		q := qs[0].(map[string]any)
		if q["header"] != "Разрешение" || q["question"] != p.Title+"\n\n"+p.Context || q["multi_select"] != false {
			t.Errorf("%s: question = %v", path, q)
		}
		opts := q["options"].([]any)
		if len(opts) != 3 || opts[0].(map[string]any)["label"] != "Yes" {
			t.Errorf("%s: options = %v", path, opts)
		}
	}
}

func TestGetSession_HookQuizHasNoSource(t *testing.T) {
	d, _ := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedQuizSession(t, d.Store, "sess1")
	_ = d.Store.SetPendingQuiz("sess1", quizAPIPendingJSON)

	resp, err := http.Get(srv.URL + "/v1/sessions/sess1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&body)
	pq := string(body["pending_quiz"])
	if strings.Contains(pq, `"source"`) || strings.Contains(pq, `"raw"`) {
		t.Errorf("hook quiz pending_quiz = %s, want no source/raw", pq)
	}
}

func TestPostQuizAnswer_PermissionDigit(t *testing.T) {
	d, rt := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedPermissionPending(t, d, rt, "permission-edit-settings.pane")

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{2}}},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	deadline := time.Now().Add(time.Second)
	for len(rt.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if sent := rt.snapshot(); len(sent) != 1 || sent[0] != "3" {
		t.Errorf("sent = %v, want [3]", sent)
	}
}

func TestPostQuizAnswer_PermissionTextIs400(t *testing.T) {
	d, rt := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedPermissionPending(t, d, rt, "permission-edit-settings.pane")

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "text": "no, do X"}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "invalid_answer" {
		t.Errorf("code = %q, want invalid_answer", code)
	}
	if sent := rt.snapshot(); len(sent) != 0 {
		t.Errorf("sent = %v, want nothing", sent)
	}
}

func TestPostQuizAnswer_PermissionPromptChangedIs409(t *testing.T) {
	d, rt := quizAnswerTestDeps(t)
	srv := newTestServer(t, d)
	seedPermissionPending(t, d, rt, "permission-bash-rm.pane")
	rt.mu.Lock()
	rt.pane = permissionPane(t, "permission-bash-outside-cwd.pane")
	rt.mu.Unlock()

	resp := postJSON(t, srv.URL+"/v1/sessions/sess1/quiz/answer", map[string]any{
		"answers": []map[string]any{{"question_index": 0, "option_indices": []int{0}}},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if code := decodeErr(t, resp).Error.Code; code != "prompt_changed" {
		t.Errorf("code = %q, want prompt_changed", code)
	}
	time.Sleep(20 * time.Millisecond)
	if sent := rt.snapshot(); len(sent) != 0 {
		t.Errorf("sent = %v, want nothing", sent)
	}
}
