package store

import "testing"

func TestQuestionBriefRoundTrip(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	const brief = "**Проблема:** staging и prod делят сеть.\n\n**Рекомендация:** развести."
	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Brief: brief, Body: "детали"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Brief != brief {
		t.Fatalf("brief = %q, want %q", q.Brief, brief)
	}

	// A thread without a brief — every thread asked before it existed — reads
	// back as an empty string, not an error.
	id, err = s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Body: "старый вопрос"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q, err = s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Brief != "" {
		t.Fatalf("brief = %q, want empty", q.Brief)
	}
}

func TestAgentQuestionBriefRoundTrip(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "sre")

	id, err := s.AddAgentQuestion(AgentQuestion{RoleID: "sre", AskedBy: "sre-run-1", Brief: "**Проблема:** диск.", Body: "как быть?"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	q, err := s.GetAgentQuestion(id)
	if err != nil {
		t.Fatalf("GetAgentQuestion: %v", err)
	}
	if q.Brief != "**Проблема:** диск." {
		t.Fatalf("brief = %q", q.Brief)
	}
}
