package runtime

import (
	"strings"
	"unicode/utf8"
)

// ParseKimiStartScreen reports whether the bottom of pane is a Kimi start
// screen waiting for the human — the trust dialog and whatever later
// versions render in the same shape — and reads it as a PermissionPrompt.
// The screen sits between two ─── rules at the bottom of the pane: a title
// row, the key hint «↑↓ navigate · Enter select …», an explanation, and a
// list of UNNUMBERED options with one ❯ cursor:
//
//	──────────────────────────
//	 Trust this folder?
//	 ↑↓ navigate · Enter select · Esc exit
//	 /path/to/the/work
//	 tree
//	  ❯ Trust and continue
//	    Exit
//	──────────────────────────
//
// Options are labels in screen order; they are chosen with arrow keys and
// Enter (KimiStartScreenCursor gives the cursor's index), never digits.
func ParseKimiStartScreen(pane string) (PermissionPrompt, bool) {
	p, _, ok := parseKimiStartScreen(pane)
	return p, ok
}

// KimiStartScreenCursor returns the index into Options of the choice the ❯
// cursor of the Kimi start screen on pane sits on, or -1 when pane shows no
// such screen.
func KimiStartScreenCursor(pane string) int {
	_, cursor, ok := parseKimiStartScreen(pane)
	if !ok {
		return -1
	}
	return cursor
}

// kimiStartHint marks the start-screen key hint row.
const kimiStartHint = "Enter select"

func isKimiRule(row string) bool { return strings.HasPrefix(strings.TrimSpace(row), "────") }

func parseKimiStartScreen(pane string) (PermissionPrompt, int, bool) {
	rows := visiblePaneRows(pane)
	last := len(rows) - 1
	if last < 0 || !isKimiRule(rows[last]) {
		return PermissionPrompt{}, -1, false
	}
	first := -1
	for i := last - 1; i >= 0 && last-i <= startHeaderRows+10; i-- {
		if isKimiRule(rows[i]) {
			first = i
			break
		}
	}
	if first < 0 {
		return PermissionPrompt{}, -1, false
	}
	body := rows[first+1 : last]

	// Options: the bottom run of rows at the option column, one ❯ cursor.
	end := len(body)
	for end > 0 && strings.TrimSpace(body[end-1]) == "" {
		end--
	}
	var options []string
	cursor, cursors := -1, 0
	col := -1
	start := end
	for start > 0 {
		r := body[start-1]
		t := strings.TrimSpace(r)
		if t == "" {
			break
		}
		indent := len(r) - len(strings.TrimLeft(r, " "))
		label, isCursor := strings.CutPrefix(t, "❯ ")
		if isCursor {
			indent += utf8.RuneCountInString("❯ ")
		}
		if col >= 0 && indent != col {
			break
		}
		col = indent
		options = append([]string{strings.TrimSpace(label)}, options...)
		if isCursor {
			cursors++
			cursor = start - 1
		}
		start--
	}
	if len(options) < 2 || cursors != 1 {
		return PermissionPrompt{}, -1, false
	}
	cursor -= start

	// Header: title, hint, explanation.
	header := trimBlankEnds(body[:start])
	if len(header) < 2 {
		return PermissionPrompt{}, -1, false
	}
	title := strings.TrimSpace(header[0])
	hint := false
	width := maxRowWidth(rows)
	var lines []string
	prev := ""
	for _, r := range header[1:] {
		if strings.Contains(r, kimiStartHint) {
			hint = true
			prev = ""
			continue
		}
		if strings.TrimSpace(r) == "" {
			lines = append(lines, "")
			prev = ""
			continue
		}
		if prev != "" {
			if sep, wrapped := wrapSeparator(prev, r, width); wrapped {
				lines[len(lines)-1] += sep + strings.TrimSpace(r)
				prev = r
				continue
			}
		}
		lines = append(lines, r)
		prev = r
	}
	if title == "" || !hint {
		return PermissionPrompt{}, -1, false
	}
	context := strings.Join(collapseBlankRuns(dedent(trimBlankEnds(lines))), "\n")
	return PermissionPrompt{
		Title:   title,
		Context: context,
		Options: options,
		Raw:     strings.Join(tailRows(rows, startRawRows), "\n"),
	}, cursor, true
}
