package activity

import "testing"

func TestStateValid(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{Active, true},
		{Ready, true},
		{Idle, true},
		{WaitingInput, true},
		{Blocked, true},
		{Exited, true},
		{State("invalid"), false},
		{State(""), false},
	}
	for _, tt := range tests {
		if got := tt.state.Valid(); got != tt.want {
			t.Errorf("State(%q).Valid() = %v, want %v", string(tt.state), got, tt.want)
		}
	}
}
