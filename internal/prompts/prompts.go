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
	result = StripMarkers(result)

	return result, nil
}

// skillsBlockRe matches a "<!-- skills:start -->" ... "<!-- skills:end -->" block,
// including the marker lines themselves and the trailing newline after each marker.
var skillsBlockRe = regexp.MustCompile(`(?s)<!-- skills:start -->\n.*?<!-- skills:end -->\n?`)

// markerLineRe matches a single skills marker line (start or end), including its
// trailing newline.
var markerLineRe = regexp.MustCompile(`(?m)^<!-- skills:(?:start|end) -->\n?`)

// tripleBlankRe collapses runs of 3+ consecutive newlines down to 2 (i.e. a single
// blank line between paragraphs).
var tripleBlankRe = regexp.MustCompile(`\n{3,}`)

// StripSkills removes every "<!-- skills:start -->" ... "<!-- skills:end -->" block
// (markers and content included) from text, then collapses the blank lines left
// behind so the result reads as coherent prose with no more than one blank line
// between paragraphs.
func StripSkills(text string) string {
	stripped := skillsBlockRe.ReplaceAllString(text, "")
	stripped = tripleBlankRe.ReplaceAllString(stripped, "\n\n")
	return stripped
}

// StripMarkers removes bare "<!-- skills:start -->" / "<!-- skills:end -->" marker
// lines from text without touching the content between them. Render always applies
// this so marker lines never leak into a rendered prompt, regardless of runtime.
func StripMarkers(text string) string {
	return markerLineRe.ReplaceAllString(text, "")
}

// Names returns the sorted list of available template names.
func Names() []string {
	return []string{"agent", "kickoff", "orchestrator", "worker"}
}
