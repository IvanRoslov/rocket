package agent

import (
	"path/filepath"
	"strings"
)

// commandBinaries maps the leading binary of a persistent agent's launcher
// command to the adapter registered for it. Detection is deliberately
// conservative: anything that is not a plainly visible binary name resolves
// to "" and the caller falls back to its default.
var commandBinaries = map[string]string{
	"kimi":        "kimi-code",
	"claude":      "claude-code",
	"claude-code": "claude-code",
	"codex":       "codex",
}

// DetectFromCommand guesses the adapter a launcher command starts, by its
// leading binary: "kimi" → kimi-code, "claude"/"claude-code" → claude-code,
// "codex" → codex. It understands an env(1) prefix and leading KEY=VAL
// shell assignments, strips directories, extensions and quotes, and returns
// "" for anything else — wrappers like "bash -c ..." hide the binary and
// must not be guessed at.
func DetectFromCommand(command string) string {
	fields := strings.Fields(command)
	for i, f := range fields {
		t := strings.Trim(f, `"'`)
		switch {
		case i == 0 && t == "env":
			continue // env(1) prefix; its KEY=VAL assignments follow
		case t == "env":
			return "" // env mid-line: not a launcher prefix we understand
		case strings.Contains(t, "=") && !strings.HasPrefix(t, "/"):
			continue // KEY=VAL assignment (after env or standalone)
		}
		name := strings.TrimSuffix(filepath.Base(t), ".exe")
		if agent, ok := commandBinaries[name]; ok {
			return agent
		}
		return ""
	}
	return ""
}
