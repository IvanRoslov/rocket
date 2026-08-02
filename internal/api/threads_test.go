package api

import (
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

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
