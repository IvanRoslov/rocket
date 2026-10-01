package codex

import (
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// codex never gets the custom skill laid out, so it must not claim it.
func TestCodexDoesNotShipBrainstormSkill(t *testing.T) {
	if agent.ShipsBrainstormSkill(New()) {
		t.Error("codex must not report shipping the orchestrator brainstorm skill")
	}
}
