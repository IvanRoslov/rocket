package runtime

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Codex CLI start screens are selection lists the TUI shows before the
// first turn: «Trust this folder?», «Update available», «Background server
// has incompatible feature settings», and whatever a later version adds in
// the same shape. All of them render a header (heading, explanation) above
// a numbered list with one › cursor, at the bottom of the pane, optionally
// followed by a footer hint («enter continue · esc quit»).
//
// Layouts: live captures of codex-cli 0.160.0 and the TUI's insta
// snapshots at rust-v0.160.0 (internal/runtime/testdata/codex-start-*).

// startOptionRow is one Codex selection choice: «› 1. Trust and continue»
// or «  2. Quit». The cursor is ›, not Claude Code's ❯.
var startOptionRow = regexp.MustCompile(`^( *)(› +)?([0-9]+)\. +(\S.*)$`)

const (
	// startTrailerRows is how many non-blank rows may follow the list
	// (the footer hint).
	startTrailerRows = 3
	// startHeaderRows bounds how far above the list the header is read.
	startHeaderRows = 20
	// startHeaderGap is how many consecutive blank rows end the header.
	startHeaderGap = 3
	// startRawRows bounds PermissionPrompt.Raw.
	startRawRows = 20
)

// startHeadingLabels are header rows that label a screen without being its
// question; the title is looked for below them.
var startHeadingLabels = map[string]bool{"Folder access": true}

// ParseStartScreen reports whether the bottom of a pane is a Codex start
// screen waiting for the human, and reads it as a PermissionPrompt: Title is
// the screen's question (the sentence ending in «?») or, without one, its
// heading; Context is the rest of the header with wrapped rows rejoined;
// Options are the numbered choices in order, so Options[i] is digit i+1 —
// Codex numbers only enabled rows, and a disabled row (shown without a
// number) is left out here too.
//
// The rule is structural: a numbered list 1..n (n ≥ 2) with exactly one ›
// cursor, nothing below it but a short footer and in particular no Codex
// composer row («› Ask Codex …»), and a header right above it. Claude
// Code's dialogs use ❯ and never match.
func ParseStartScreen(pane string) (PermissionPrompt, bool) {
	rows := visiblePaneRows(pane)
	if len(rows) == 0 {
		return PermissionPrompt{}, false
	}

	last := -1
	trailer := 0
	for i := len(rows) - 1; i >= 0; i-- {
		if startOptionRow.MatchString(rows[i]) {
			last = i
			break
		}
		t := strings.TrimSpace(rows[i])
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "›") || isFrameRule(rows[i]) {
			// the composer or a frame below the list: not a start screen
			return PermissionPrompt{}, false
		}
		trailer++
		if trailer > startTrailerRows {
			return PermissionPrompt{}, false
		}
	}
	if last < 0 {
		return PermissionPrompt{}, false
	}

	// Walk up over the list: numbered rows and their indented
	// continuations (wrapped labels, disabled rows).
	top := last
	for top > 0 {
		up := rows[top-1]
		if startOptionRow.MatchString(up) || isStartContinuation(up) {
			top--
			continue
		}
		break
	}
	for top < last && !startOptionRow.MatchString(rows[top]) {
		top++
	}

	width := maxRowWidth(rows)
	options, ok := readStartOptions(rows[top:last+1], width)
	if !ok {
		return PermissionPrompt{}, false
	}

	header := startHeader(rows[:top], width)
	if len(header) == 0 {
		return PermissionPrompt{}, false
	}
	title, context := startTitle(header)
	if title == "" {
		return PermissionPrompt{}, false
	}

	return PermissionPrompt{
		Title:   title,
		Context: context,
		Options: options,
		Raw:     strings.Join(tailRows(rows, startRawRows), "\n"),
	}, true
}

// isStartContinuation matches a non-numbered row inside a list: indented
// (Codex indents labels past the number) and not blank.
func isStartContinuation(row string) bool {
	if strings.TrimSpace(row) == "" {
		return false
	}
	return strings.HasPrefix(row, "   ")
}

// readStartOptions folds the list rows into labels. Numbered rows must run
// 1..n (n ≥ 2) with exactly one cursor. A continuation row is joined to the
// label only when it is the TUI's wrap of it; any other sub-row (a disabled
// choice, a description) is skipped — it has no digit of its own.
func readStartOptions(rows []string, width int) ([]string, bool) {
	var labels []string
	cursors := 0
	prev := ""
	for _, r := range rows {
		if m := startOptionRow.FindStringSubmatch(r); m != nil {
			n, err := strconv.Atoi(m[3])
			if err != nil || n != len(labels)+1 {
				return nil, false
			}
			if m[2] != "" {
				cursors++
			}
			labels = append(labels, strings.TrimSpace(m[4]))
			prev = r
			continue
		}
		if prev != "" {
			if sep, wrapped := wrapSeparator(prev, r, width); wrapped {
				labels[len(labels)-1] += sep + strings.TrimSpace(r)
				prev = r
				continue
			}
		}
		prev = ""
	}
	if len(labels) < 2 || cursors != 1 {
		return nil, false
	}
	return labels, true
}

// startHeader returns the screen's header above the list as paragraphs of
// dedented lines, wrapped rows rejoined: the run of indented rows up to
// startHeaderGap blank rows, an unindented row (shell output above the
// TUI), or startHeaderRows rows.
func startHeader(rows []string, width int) []string {
	start := len(rows)
	blanks := 0
	for start > 0 && len(rows)-start < startHeaderRows {
		r := rows[start-1]
		if strings.TrimSpace(r) == "" {
			blanks++
			if blanks >= startHeaderGap {
				break
			}
			start--
			continue
		}
		if !strings.HasPrefix(r, " ") || isFrameRule(r) {
			break
		}
		blanks = 0
		start--
	}

	var lines []string
	prev := ""
	for _, r := range rows[start:] {
		if strings.TrimSpace(r) == "" {
			lines = append(lines, "")
			prev = ""
			continue
		}
		// A row filling less than half the width is a line of its own (a
		// heading), even when the next row's long word could not have
		// followed it.
		if prev != "" && 2*utf8.RuneCountInString(prev) >= width {
			if sep, wrapped := wrapSeparator(prev, r, width); wrapped {
				lines[len(lines)-1] += sep + strings.TrimSpace(r)
				prev = r
				continue
			}
		}
		lines = append(lines, r)
		prev = r
	}
	lines = dedent(trimBlankEnds(lines))
	return collapseBlankRuns(lines)
}

// startTitle picks the screen's title out of its header lines and returns
// the rest as context. The first sentence ending in «?» is the title (the
// trust screen's question opens a longer paragraph, which stays in the
// context whole); without one, the first line that is not a bare heading
// label is.
func startTitle(header []string) (string, string) {
	for _, l := range header {
		if q := questionSentence(l); q != "" {
			return q, strings.Join(header, "\n")
		}
	}
	for i, l := range header {
		t := strings.TrimSpace(l)
		if t == "" || startHeadingLabels[t] {
			continue
		}
		rest := append(append([]string{}, header[:i]...), header[i+1:]...)
		return t, strings.Join(trimBlankEnds(collapseBlankRuns(rest)), "\n")
	}
	return "", ""
}

// questionSentence returns the first sentence of line that ends in «?»
// followed by a space or the end of line, or "".
func questionSentence(line string) string {
	t := strings.TrimSpace(line)
	for i, r := range t {
		if r != '?' {
			continue
		}
		end := i + utf8.RuneLen(r)
		if end < len(t) && t[end] != ' ' {
			continue
		}
		start := 0
		for j := i - 1; j > 0; j-- {
			if (t[j] == '.' || t[j] == '!' || t[j] == '?') && t[j+1] == ' ' {
				start = j + 2
				break
			}
		}
		return strings.TrimSpace(t[start:end])
	}
	return ""
}

func collapseBlankRuns(lines []string) []string {
	out := lines[:0:0]
	for i, l := range lines {
		if l == "" && i > 0 && lines[i-1] == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}
