package store

import "testing"

// seedAgentForQuestions registers a role (and its project on first call) so
// question threads have something to hang off.
func seedAgentForQuestions(t *testing.T, s *Store, id string) {
	t.Helper()
	if _, err := s.GetProject("platform"); err == ErrNotFound {
		addAgentFixtures(t, s)
	}
	if err := s.AddAgent(Agent{ID: id, ProjectID: "platform", PromptPath: "/tmp/role.md", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
}

func TestAgentQuestionThreadLifecycle(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	id, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "как быть?"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}

	q, err := s.GetAgentQuestion(id)
	if err != nil {
		t.Fatalf("GetAgentQuestion: %v", err)
	}
	if q.Status != "open" || q.RoleID != "sre" || q.AskedBy != "" || q.AskedAt == 0 {
		t.Fatalf("unexpected question: %+v", q)
	}

	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: id, Author: "sre-run-1", Body: "смотрю"}); err != nil {
		t.Fatalf("AddAgentQuestionMessage: %v", err)
	}
	msgs, err := s.ListAgentQuestionMessages(id)
	if err != nil {
		t.Fatalf("ListAgentQuestionMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Kind != "reply" || msgs[0].Author != "sre-run-1" || msgs[0].CreatedAt == 0 {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	if err := s.ResolveAgentQuestion(id, "answered"); err != nil {
		t.Fatalf("ResolveAgentQuestion: %v", err)
	}
	if err := s.ResolveAgentQuestion(id, "answered"); err != ErrQuestionResolved {
		t.Fatalf("double resolve = %v, want ErrQuestionResolved", err)
	}
	q, _ = s.GetAgentQuestion(id)
	if q.Status != "resolved" || q.Resolution != "answered" || q.ResolvedAt == 0 {
		t.Fatalf("after resolve: %+v", q)
	}

	if err := s.ReopenAgentQuestion(id); err != nil {
		t.Fatalf("ReopenAgentQuestion: %v", err)
	}
	if err := s.ReopenAgentQuestion(id); err != ErrQuestionOpen {
		t.Fatalf("double reopen = %v, want ErrQuestionOpen", err)
	}
	q, _ = s.GetAgentQuestion(id)
	if q.Status != "open" || q.Resolution != "" || q.ResolvedAt != 0 {
		t.Fatalf("after reopen: %+v", q)
	}
}

func TestGetAgentQuestionMissing(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetAgentQuestion(9999); err != ErrNotFound {
		t.Fatalf("missing question = %v, want ErrNotFound", err)
	}
	if err := s.ResolveAgentQuestion(9999, "answered"); err != ErrNotFound {
		t.Fatalf("resolve missing = %v, want ErrNotFound", err)
	}
}

func TestListAgentQuestionsAndOrdinal(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "sre")
	seedAgentForQuestions(t, s, "triage")

	q1, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q1"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	q2, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q2"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if _, err := s.AddAgentQuestion(AgentQuestion{RoleID: "triage", Body: "чужой"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if err := s.ResolveAgentQuestion(q1, "dismissed"); err != nil {
		t.Fatalf("ResolveAgentQuestion: %v", err)
	}

	all, err := s.ListAgentQuestions("sre", false)
	if err != nil || len(all) != 2 {
		t.Fatalf("list all = %+v, %v", all, err)
	}
	open, err := s.ListAgentQuestions("sre", true)
	if err != nil || len(open) != 1 || open[0].ID != q2 {
		t.Fatalf("list open = %+v, %v", open, err)
	}

	second, _ := s.GetAgentQuestion(q2)
	n, err := s.AgentQuestionOrdinal(second)
	if err != nil || n != 2 {
		t.Fatalf("ordinal = %d, %v; want 2", n, err)
	}
}

func TestOpenAgentQuestionCounts(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	// Role-opened, no reply yet: awaits the human.
	if _, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", AskedBy: "sre-run-1", Body: "нужно решение"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	// Human-opened, no reply yet: awaits the role.
	if _, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "как дела?"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	// Human-opened but the role spoke last: awaits the human again.
	replied, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "и ещё"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: replied, Author: "sre-run-1", Body: "ответ"}); err != nil {
		t.Fatalf("AddAgentQuestionMessage: %v", err)
	}
	// Resolved threads never count.
	done, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "старое"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if err := s.ResolveAgentQuestion(done, "answered"); err != nil {
		t.Fatalf("ResolveAgentQuestion: %v", err)
	}

	counts, err := s.OpenAgentQuestionCounts()
	if err != nil {
		t.Fatalf("OpenAgentQuestionCounts: %v", err)
	}
	got := counts["sre"]
	if got.Open != 3 || got.AwaitingUser != 2 {
		t.Fatalf("counts = %+v; want {Open:3 AwaitingUser:2}", got)
	}
}

func TestDeleteAgentPurgesQuestions(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	qid, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", Body: "q"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if _, err := s.AddAgentQuestionMessage(AgentQuestionMessage{QuestionID: qid, Body: "m"}); err != nil {
		t.Fatalf("AddAgentQuestionMessage: %v", err)
	}

	if err := s.DeleteAgent("sre"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := s.GetAgentQuestion(qid); err != ErrNotFound {
		t.Fatalf("question survived delete: %v", err)
	}
	msgs, err := s.ListAgentQuestionMessages(qid)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("messages survived delete: %+v, %v", msgs, err)
	}
}
