package runtime

import (
	"strings"
	"unicode/utf8"
)

// PaneDialectPi selects Pi's composer and steering parsers wherever rocket
// reads or types into a pane.
const PaneDialectPi = "pi"

type PiComposer int

const (
	PiEmpty PiComposer = iota
	PiDraft
	PiBusy
)

func (c PiComposer) String() string {
	switch c {
	case PiEmpty:
		return "empty"
	case PiDraft:
		return "draft"
	case PiBusy:
		return "busy"
	}
	return "unknown"
}

type piComposer struct {
	state PiComposer
	queue []string
}

func ParsePiComposer(pane string) (PiComposer, bool) {
	c, ok := parsePiComposer(pane)
	return c.state, ok
}

func isPiRule(row string) bool { return strings.HasPrefix(strings.TrimSpace(row), "────") }

func isPiWorking(row string) bool {
	row = strings.TrimSpace(row)
	return strings.HasPrefix(row, "── ") && strings.Contains(row, " Working ")
}

func parsePiComposer(pane string) (piComposer, bool) {
	rows := trimTrailingBlankLines(parsePane(pane))
	if len(rows) < 4 {
		return piComposer{}, false
	}
	model := strings.TrimSpace(rows[len(rows)-1].text())
	if !strings.Contains(model, " • ") || !strings.Contains(model, "(auto)") || !strings.Contains(model, "%/") {
		return piComposer{}, false
	}
	worktree := strings.TrimSpace(rows[len(rows)-2].text())
	if !strings.Contains(worktree, "(") || !strings.Contains(worktree, ")") {
		return piComposer{}, false
	}

	bottom := len(rows) - 3
	for bottom >= 0 && strings.TrimSpace(rows[bottom].text()) == "" {
		bottom--
	}
	if bottom < 0 || !isPiRule(rows[bottom].text()) {
		return piComposer{}, false
	}
	previous := bottom - 1
	for previous >= 0 && strings.TrimSpace(rows[previous].text()) == "" {
		previous--
	}
	if previous >= 0 && isPiWorking(rows[previous].text()) {
		return piComposer{state: PiBusy, queue: piSteering(rows[:previous])}, true
	}

	top := -1
	var content []string
	for i := previous; i >= 0; i-- {
		row := rows[i].text()
		if isPiRule(row) {
			top = i
			break
		}
		content = append([]string{row}, content...)
	}
	if top < 0 {
		return piComposer{}, false
	}
	for _, row := range content {
		if strings.TrimSpace(row) != "" {
			return piComposer{state: PiDraft}, true
		}
	}
	return piComposer{state: PiEmpty}, true
}

func piSteering(rows []paneLine) []string {
	var queue []string
	for i := len(rows) - 1; i >= 0; i-- {
		row := strings.TrimSpace(rows[i].text())
		if isPiRule(row) {
			break
		}
		if row == "" || strings.HasPrefix(row, "↳ Option+Up") {
			continue
		}
		if text, ok := strings.CutPrefix(row, "Steering:"); ok {
			queue = append([]string{strings.TrimSpace(text)}, queue...)
			continue
		}
		break
	}
	return queue
}

func LooksLikePiQueued(pane, text string) bool {
	want := stripWhitespace(text)
	if want == "" {
		return false
	}
	c, ok := parsePiComposer(pane)
	if !ok || c.state != PiBusy {
		return false
	}
	for _, item := range c.queue {
		got := stripWhitespace(item)
		if got == want {
			return true
		}
		head, cut := strings.CutSuffix(got, "…")
		if cut && utf8.RuneCountInString(head) >= minQueuedPrefix && strings.HasPrefix(want, head) {
			return true
		}
	}
	return false
}
