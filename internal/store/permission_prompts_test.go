package store

import "testing"

func addPermissionTestSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.AddSession(Session{
		ID: id, Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b-" + id,
		WorktreePath: "/wt/" + id, TmuxName: "t-" + id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

func getPermissionRow(t *testing.T, s *Store, id int64) PermissionPromptRow {
	t.Helper()
	var r PermissionPromptRow
	var resolved *int64
	err := s.db.QueryRow(`SELECT id, session_id, title, context, options_json, asked_at, resolved_at, answer_label, answered_via
		FROM permission_prompts WHERE id = ?`, id).Scan(&r.ID, &r.SessionID, &r.Title, &r.Context, &r.OptionsJSON, &r.AskedAt, &resolved, &r.AnswerLabel, &r.AnsweredVia)
	if err != nil {
		t.Fatalf("read row %d: %v", id, err)
	}
	if resolved != nil {
		r.ResolvedAt = *resolved
	}
	return r
}

func countPermissionRows(t *testing.T, s *Store, sessionID string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM permission_prompts WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestOpenPermissionPromptReusesOpenRowWithSameIdentity(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	id1, reused, err := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "rm -rf build", `["Yes","No"]`, 100)
	if err != nil || reused {
		t.Fatalf("first open: id=%d reused=%v err=%v", id1, reused, err)
	}
	id2, reused, err := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "rm -rf build", `["Yes","No"]`, 200)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if !reused || id2 != id1 {
		t.Errorf("second open = (%d, reused=%v), want (%d, true)", id2, reused, id1)
	}
	if n := countPermissionRows(t, s, "s1"); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	if r := getPermissionRow(t, s, id1); r.AskedAt != 100 || r.ResolvedAt != 0 {
		t.Errorf("row = %+v, want asked_at 100 and still open", r)
	}
}

func TestOpenPermissionPromptClosesPreviousOpenRow(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")
	addPermissionTestSession(t, s, "s2")

	first, _, _ := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "rm -rf build", `[]`, 100)
	other, _, _ := s.OpenPermissionPrompt("s2", "Do you want to proceed?", "ls", `[]`, 100)
	second, reused, err := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "ls /etc", `[]`, 150)
	if err != nil || reused || second == first {
		t.Fatalf("open different identity: id=%d reused=%v err=%v", second, reused, err)
	}

	r := getPermissionRow(t, s, first)
	if r.ResolvedAt == 0 || r.AnsweredVia != "terminal" || r.AnswerLabel != "" {
		t.Errorf("previous row = %+v, want resolved via terminal", r)
	}
	if r := getPermissionRow(t, s, other); r.ResolvedAt != 0 {
		t.Errorf("other session's row was closed: %+v", r)
	}
	if r := getPermissionRow(t, s, second); r.ResolvedAt != 0 {
		t.Errorf("new row closed: %+v", r)
	}
}

func TestOpenPermissionPromptClosesAnsweredRowAsChat(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	first, _, _ := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "a", `[]`, 100)
	if err := s.MarkPermissionPromptAnswered(first, "Yes"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if _, _, err := s.OpenPermissionPrompt("s1", "Do you want to proceed?", "b", `[]`, 150); err != nil {
		t.Fatalf("open: %v", err)
	}
	r := getPermissionRow(t, s, first)
	if r.AnsweredVia != "chat" || r.AnswerLabel != "Yes" || r.ResolvedAt == 0 {
		t.Errorf("row = %+v, want resolved via chat with label Yes", r)
	}
}

func TestResolvePermissionPrompt(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	plain, _, _ := s.OpenPermissionPrompt("s1", "T1?", "", `[]`, 100)
	if err := s.ResolvePermissionPrompt(plain, 300); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r := getPermissionRow(t, s, plain); r.ResolvedAt != 300 || r.AnsweredVia != "terminal" {
		t.Errorf("row = %+v, want resolved_at 300 via terminal", r)
	}
	// Idempotent: a second resolve keeps the first resolution.
	if err := s.ResolvePermissionPrompt(plain, 400); err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if r := getPermissionRow(t, s, plain); r.ResolvedAt != 300 {
		t.Errorf("resolved_at = %d after second resolve, want 300", r.ResolvedAt)
	}

	answered, _, _ := s.OpenPermissionPrompt("s1", "T2?", "", `[]`, 500)
	_ = s.MarkPermissionPromptAnswered(answered, "Esc")
	if err := s.ResolvePermissionPrompt(answered, 600); err != nil {
		t.Fatalf("resolve answered: %v", err)
	}
	if r := getPermissionRow(t, s, answered); r.AnsweredVia != "chat" || r.AnswerLabel != "Esc" {
		t.Errorf("row = %+v, want via chat label Esc", r)
	}
}

func TestMarkPermissionPromptAnsweredEmptyLabelClears(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	id, _, _ := s.OpenPermissionPrompt("s1", "T?", "", `[]`, 100)
	_ = s.MarkPermissionPromptAnswered(id, "Yes")
	if err := s.MarkPermissionPromptAnswered(id, ""); err != nil {
		t.Fatalf("clear mark: %v", err)
	}
	_ = s.ResolvePermissionPrompt(id, 200)
	if r := getPermissionRow(t, s, id); r.AnsweredVia != "terminal" || r.AnswerLabel != "" {
		t.Errorf("row = %+v, want via terminal without label", r)
	}
}

func TestListResolvedPermissionPrompts(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")
	addPermissionTestSession(t, s, "s2")

	a, _, _ := s.OpenPermissionPrompt("s1", "A?", "ctx a", `["Yes"]`, 100)
	_ = s.MarkPermissionPromptAnswered(a, "Yes")
	b, _, _ := s.OpenPermissionPrompt("s1", "B?", "", `[]`, 200) // closes a
	_ = s.ResolvePermissionPrompt(b, 250)
	_, _, _ = s.OpenPermissionPrompt("s1", "C?", "", `[]`, 300) // open: not listed
	o, _, _ := s.OpenPermissionPrompt("s2", "O?", "", `[]`, 100)
	_ = s.ResolvePermissionPrompt(o, 150)

	rows, err := s.ListResolvedPermissionPrompts("s1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != a || rows[1].ID != b {
		t.Fatalf("rows = %+v, want [a, b]", rows)
	}
	if rows[0].Title != "A?" || rows[0].Context != "ctx a" || rows[0].OptionsJSON != `["Yes"]` ||
		rows[0].AskedAt != 100 || rows[0].AnswerLabel != "Yes" || rows[0].AnsweredVia != "chat" || rows[0].ResolvedAt == 0 {
		t.Errorf("row a = %+v", rows[0])
	}
	if rows[1].AnsweredVia != "terminal" || rows[1].ResolvedAt != 250 {
		t.Errorf("row b = %+v", rows[1])
	}
}

func TestCompareAndSwapPendingQuiz(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	// NULL -> value.
	ok, err := s.CompareAndSwapPendingQuiz("s1", "", `{"a":1}`)
	if err != nil || !ok {
		t.Fatalf("swap from empty: ok=%v err=%v", ok, err)
	}
	// Mismatch: no change.
	ok, err = s.CompareAndSwapPendingQuiz("s1", "", `{"b":2}`)
	if err != nil || ok {
		t.Fatalf("swap with stale old: ok=%v err=%v, want false nil", ok, err)
	}
	if got, _ := s.GetSession("s1"); got.PendingQuiz != `{"a":1}` {
		t.Fatalf("pending = %q after failed swap", got.PendingQuiz)
	}
	// value -> NULL.
	ok, err = s.CompareAndSwapPendingQuiz("s1", `{"a":1}`, "")
	if err != nil || !ok {
		t.Fatalf("swap to empty: ok=%v err=%v", ok, err)
	}
	if got, _ := s.GetSession("s1"); got.PendingQuiz != "" {
		t.Fatalf("pending = %q, want empty", got.PendingQuiz)
	}
}

func TestMarkPermissionPromptSentAndGet(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	id, _, _ := s.OpenPermissionPrompt("s1", "T?", "c", `["Yes"]`, 100)
	r, err := s.GetPermissionPrompt(id)
	if err != nil || r.SentAt != 0 || r.Title != "T?" || r.ResolvedAt != 0 {
		t.Fatalf("fresh row = %+v err=%v", r, err)
	}
	if err := s.MarkPermissionPromptSent(id, 150); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.GetPermissionPrompt(id); r.SentAt != 150 {
		t.Errorf("sent_at = %d, want 150", r.SentAt)
	}
	if _, err := s.GetPermissionPrompt(9999); err != ErrNotFound {
		t.Errorf("missing row err = %v, want ErrNotFound", err)
	}
}

func TestResolveOpenPermissionPrompts(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")
	addPermissionTestSession(t, s, "s2")

	a, _, _ := s.OpenPermissionPrompt("s1", "A?", "", `[]`, 100)
	o, _, _ := s.OpenPermissionPrompt("s2", "O?", "", `[]`, 100)
	if err := s.ResolveOpenPermissionPrompts("s1", 200); err != nil {
		t.Fatal(err)
	}
	if r := getPermissionRow(t, s, a); r.ResolvedAt != 200 || r.AnsweredVia != "terminal" {
		t.Errorf("s1 row = %+v, want closed via terminal at 200", r)
	}
	if r := getPermissionRow(t, s, o); r.ResolvedAt != 0 {
		t.Errorf("s2 row closed: %+v", r)
	}
}

func TestDeletePermissionPrompt(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)
	addPermissionTestSession(t, s, "s1")

	id, _, _ := s.OpenPermissionPrompt("s1", "A?", "", `[]`, 100)
	if err := s.DeletePermissionPrompt(id); err != nil {
		t.Fatal(err)
	}
	if n := countPermissionRows(t, s, "s1"); n != 0 {
		t.Errorf("rows = %d after delete, want 0", n)
	}
}
