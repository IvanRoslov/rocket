package agent

import (
	"fmt"
)

// Registry returns a map of all registered agents.
func Registry() map[string]Agent {
	result := make(map[string]Agent)
	for name, builder := range builders {
		result[name] = builder()
	}
	return result
}

// Get returns an agent by name, or an error if not found.
func Get(name string) (Agent, error) {
	builder, ok := builders[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", name)
	}
	return builder(), nil
}
