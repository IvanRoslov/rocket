package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed templates/*
var templates embed.FS

// Vars is a map of template variable names to their values.
type Vars map[string]string

// Render loads the template (override file <home>/prompts/<name>.md if it exists, else embedded),
// replaces every {{key}} with v[key], and errors if any required placeholder is missing from Vars.
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

	// Extract all placeholders from the template
	placeholderRe := regexp.MustCompile(`\{\{([a-z_]+)\}\}`)
	matches := placeholderRe.FindAllStringSubmatch(content, -1)

	// Collect all missing variable names
	var missing []string
	seen := make(map[string]bool)
	for _, match := range matches {
		varName := match[1]
		if !seen[varName] {
			if _, ok := v[varName]; !ok {
				missing = append(missing, varName)
				seen[varName] = true
			}
		}
	}

	// Error if any variables are missing
	if len(missing) > 0 {
		sort.Strings(missing)
		placeholders := make([]string, len(missing))
		for i, varName := range missing {
			placeholders[i] = "{{" + varName + "}}"
		}
		return "", fmt.Errorf("unresolved placeholders in template %q: %s", name, strings.Join(placeholders, ", "))
	}

	// Build replacer from Vars and perform substitution
	pairs := make([]string, 0, len(v)*2)
	for key, value := range v {
		pairs = append(pairs, "{{"+key+"}}", value)
	}
	replacer := strings.NewReplacer(pairs...)
	result := replacer.Replace(content)

	return result, nil
}

// Names returns the sorted list of available template names.
func Names() []string {
	return []string{"kickoff", "orchestrator", "worker"}
}
