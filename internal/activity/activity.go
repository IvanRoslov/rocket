package activity

// State represents the activity state of an orchestrator or worker session.
type State string

const (
	Active       State = "active"
	Ready        State = "ready"
	Idle         State = "idle"
	WaitingInput State = "waiting_input"
	Blocked      State = "blocked"
	Exited       State = "exited"
)

// Valid returns true if the state is a valid activity state.
func (s State) Valid() bool {
	switch s {
	case Active, Ready, Idle, WaitingInput, Blocked, Exited:
		return true
	default:
		return false
	}
}
