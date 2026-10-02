// Package threadtext renders thread parts that must read the same in the
// CLI and in text delivered to agents.
package threadtext

import (
	"fmt"
	"strings"
)

// OptionsLine renders a thread's answer choices numbered as "--choose <n>"
// indexes them; the recommended one (1-based, 0 = none) is marked. It returns
// "" when there are no options.
func OptionsLine(options []string, recommended int) string {
	if len(options) == 0 {
		return ""
	}
	parts := make([]string, len(options))
	for i, o := range options {
		parts[i] = fmt.Sprintf("%d) %s", i+1, o)
		if i+1 == recommended {
			parts[i] += " ★ рекомендовано"
		}
	}
	return "варианты: " + strings.Join(parts, "  ")
}
