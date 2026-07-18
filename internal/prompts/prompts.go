package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templates embed.FS

// Vars is a map of template variable names to their values.
type Vars map[string]string

// Render loads the template (override file <home>/prompts/<name>.md if it exists, else embedded),
// replaces every {{key}} with v[key], and errors if any {{...}} placeholder remains
// (except {{project_rules}} which may legitimately be empty string).
func Render(home, name string, v Vars) (string, error) {
	var content string

	// Try override file first
	if home != "" {
		overridePath := filepath.Join(home, "prompts", name+".md")
		if data, readErr := os.ReadFile(overridePath); readErr == nil {
			content = string(data)
		} else if !os.IsNotExist(readErr) {
			return "", fmt.Errorf("failed to read override template %q: %w", overridePath, readErr)
		}
	}

	// If no override, load embedded template
	if content == "" {
		data, err := templates.ReadFile(filepath.Join("templates", name+".md"))
		if err != nil {
			return "", fmt.Errorf("failed to load template %q: %w", name, err)
		}
		content = string(data)
	}

	// Build replacer from Vars
	pairs := make([]string, 0, len(v)*2)
	for key, value := range v {
		pairs = append(pairs, "{{"+key+"}}", value)
	}
	replacer := strings.NewReplacer(pairs...)
	result := replacer.Replace(content)

	// Check for unresolved placeholders
	if idx := strings.Index(result, "{{"); idx != -1 {
		// Extract the placeholder name
		endIdx := strings.Index(result[idx:], "}}")
		if endIdx == -1 {
			return "", fmt.Errorf("unresolved placeholder in template %q: malformed placeholder at position %d", name, idx)
		}
		placeholder := result[idx : idx+endIdx+2]
		return "", fmt.Errorf("unresolved placeholder in template %q: %s", name, placeholder)
	}

	return result, nil
}

// Names returns the sorted list of available template names.
func Names() []string {
	return []string{"kickoff", "orchestrator", "worker"}
}
