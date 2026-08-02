package api

import (
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentCaller builds the caller a persistent agent presents: a session
// registered under the agent's own id with kind=agent.
func agentCaller(id string) *store.Session {
	return &store.Session{ID: id, Kind: session.AgentSessionKind}
}

func TestCallerParticipant_HumanIsCanonical(t *testing.T) {
	if got := callerParticipant(nil); got != store.ParticipantHuman {
		t.Errorf("callerParticipant(nil) = %q, want %q", got, store.ParticipantHuman)
	}
	if got := callerParticipant(&store.Session{ID: "orch-1"}); got != "orch-1" {
		t.Errorf("callerParticipant(orch-1) = %q, want orch-1", got)
	}
}

func TestWaitingOn(t *testing.T) {
	parts := []string{"cto", "human", "orch-1"}

	tests := []struct {
		name string
		q    store.Question
		msgs []store.QuestionMessage
		want []string
	}{
		{
			name: "resolved thread waits on nobody",
			q:    store.Question{Status: "resolved", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: "human"}},
			want: nil,
		},
		{
			name: "no messages, orchestrator asked: everyone but the asker",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			want: []string{"cto", "human"},
		},
		{
			name: "no messages, human asked (asked_by is empty)",
			q:    store.Question{Status: "open", AskedBy: ""},
			want: []string{"cto", "orch-1"},
		},
		{
			name: "last message unaddressed: everyone but its author",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: "cto"}},
			want: []string{"human", "orch-1"},
		},
		{
			name: "last message addressed: exactly its addressees",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{
				{Author: "human"},
				{Author: "cto", AddressedTo: []string{"orch-1"}},
			},
			want: []string{"orch-1"},
		},
		{
			name: "human author stored as the legacy empty string",
			q:    store.Question{Status: "open", AskedBy: "orch-1"},
			msgs: []store.QuestionMessage{{Author: ""}},
			want: []string{"cto", "orch-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waitingOn(tt.q, tt.msgs, parts)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("waitingOn = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWhoseTurnCompat(t *testing.T) {
	if got := whoseTurnCompat([]string{"cto", "human"}, "orchestrator"); got != "user" {
		t.Errorf("with human = %q, want user", got)
	}
	if got := whoseTurnCompat([]string{"cto"}, "orchestrator"); got != "orchestrator" {
		t.Errorf("without human = %q, want orchestrator", got)
	}
	if got := whoseTurnCompat([]string{"cto"}, "role"); got != "role" {
		t.Errorf("role vocabulary = %q, want role", got)
	}
	if got := whoseTurnCompat(nil, "orchestrator"); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestCanAnswerThread(t *testing.T) {
	d := Deps{}
	if !canAnswerThread(d, nil) {
		t.Error("the human must be able to answer")
	}
	if !canAnswerThread(d, agentCaller("cto")) {
		t.Error("a persistent agent must be able to answer")
	}
	if canAnswerThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}) {
		t.Error("an orchestrator must not be able to answer")
	}
	if canAnswerThread(d, &store.Session{ID: "w-1", Kind: "worker"}) {
		t.Error("a worker must not be able to answer")
	}
}

func TestCanPostToThread(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}
	parts := []string{"cto", "human", "orch-1"}

	if !canPostToThread(d, nil, subj, parts) {
		t.Error("the human must be able to post")
	}
	if !canPostToThread(d, agentCaller("cto"), subj, parts) {
		t.Error("a participant agent must be able to post")
	}
	if !canPostToThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}, subj, parts) {
		t.Error("the task's own orchestrator must be able to post")
	}
	if canPostToThread(d, &store.Session{ID: "w-9", Kind: "worker"}, subj, parts) {
		t.Error("a non-participant worker must not be able to post")
	}
	if !canPostToThread(d, &store.Session{ID: "w-9", Kind: "worker"},
		subj, append(parts, "w-9")) {
		t.Error("a worker that is a participant must be able to post")
	}
}

func TestCanOpenThread(t *testing.T) {
	d := Deps{}
	subj := threadSubject{TaskID: 7, Counterpart: "orch-1"}

	if !canOpenThread(d, nil, subj) {
		t.Error("the human must be able to open a thread")
	}
	if !canOpenThread(d, agentCaller("cto"), subj) {
		t.Error("a persistent agent must be able to open a thread")
	}
	if !canOpenThread(d, &store.Session{ID: "orch-1", Kind: "orchestrator"}, subj) {
		t.Error("the task's own orchestrator must be able to open a thread")
	}
	if canOpenThread(d, &store.Session{ID: "orch-2", Kind: "orchestrator"}, subj) {
		t.Error("another task's orchestrator must not be able to open a thread")
	}
	if canOpenThread(d, &store.Session{ID: "w-1", Kind: "worker"}, subj) {
		t.Error("a worker must not be able to open a thread")
	}
}
