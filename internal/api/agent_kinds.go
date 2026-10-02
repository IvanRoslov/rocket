package api

import (
	"net/http"
	"sort"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// agentKindResponse is one registered agent implementation (claude-code,
// codex, ...) as offered to the UI's orchestrator picker. Available says
// whether the agent's executable is usable on this machine right now;
// Error carries the reason when it is not.
type agentKindResponse struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
	// Efforts are the reasoning-effort levels a model profile of this agent
	// may set (always an array, empty when the agent has none).
	Efforts []string `json:"efforts"`
}

// agentKindsResponse is the payload of GET /v1/agent-kinds: the registry
// plus the daemon's default, so the picker can label the empty choice.
type agentKindsResponse struct {
	Kinds   []agentKindResponse `json:"kinds"`
	Default string              `json:"default"`
}

// registerAgentKindRoutes wires GET /v1/agent-kinds onto mux.
func registerAgentKindRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/agent-kinds", func(w http.ResponseWriter, r *http.Request) {
		kinds := make([]agentKindResponse, 0, 4)
		for name, a := range agent.Registry() {
			k := agentKindResponse{Name: name, Efforts: a.Efforts()}
			if k.Efforts == nil {
				k.Efforts = []string{}
			}
			if err := a.Available(); err != nil {
				k.Error = err.Error()
			} else {
				k.Available = true
			}
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i].Name < kinds[j].Name })
		writeJSON(w, http.StatusOK, agentKindsResponse{Kinds: kinds, Default: d.Cfg.DefaultAgent})
	})
}
