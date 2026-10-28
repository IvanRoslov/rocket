package agent

import "testing"

// plainAgent implements Agent (through the nil embedded interface) and none
// of the optional capabilities.
type plainAgent struct{ Agent }

type busyAgent struct {
	Agent
	busy bool
}

func (b busyAgent) AcceptsInputWhileBusy() bool { return b.busy }

func TestAcceptsInputWhileBusy(t *testing.T) {
	if AcceptsInputWhileBusy(plainAgent{}) {
		t.Fatal("an agent without BusyInputAccepter must not accept input while busy")
	}
	if AcceptsInputWhileBusy(busyAgent{busy: false}) {
		t.Fatal("AcceptsInputWhileBusy() == false must be honoured")
	}
	if !AcceptsInputWhileBusy(busyAgent{busy: true}) {
		t.Fatal("AcceptsInputWhileBusy() == true must be honoured")
	}
}
