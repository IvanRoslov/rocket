package runtime

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PermissionPrompt is a Claude Code permission dialog read off a pane: the
// tool-permission prompts («Do you want to proceed?», «Do you want to make
// this edit to …?») and the ExitPlanMode approval («… Would you like to
// proceed?»). See docs/superpowers/recon/2026-10-01-permission-prompt-recon.md
// for the layouts this is built on.
type PermissionPrompt struct {
	// Title is the dialog's question, e.g. "Do you want to make this edit
	// to settings.json?".
	Title string
	// Context is up to permissionContextLines lines above the title inside
	// the dialog frame (tool name, command, path, diff); may be empty.
	Context string
	// Options are the choices in order, without the "N. " prefix and the ❯
	// cursor, so Options[i] is answered by the digit key i+1. Empty when
	// the choices could not be mapped to digit keys — the client then shows
	// Raw and offers Esc only.
	Options []string
	// Raw is the bottom permissionRawRows rows of the pane, escapes
	// stripped, for the client to show when Options is empty.
	Raw string
}

// PermissionCaptureLines is how many bottom pane rows are read to look for
// a permission dialog. Every reader that compares dialogs (the monitor, and
// the answer path's re-read right before the keypress) must capture this
// same depth: when the dialog's top rule is cut off, Context falls back to
// the rows nearest the title, so two depths can disagree about one dialog.
// 40 rows hold the full header of every recon capture; 15 cut big diffs.
const PermissionCaptureLines = 40

// SameDialog reports whether p and q are the same dialog: same Title and
// Context. Options and Raw are not part of the identity (the cursor moving
// changes Raw). Context matters because consecutive Bash dialogs share the
// title «Do you want to proceed?».
func (p PermissionPrompt) SameDialog(q PermissionPrompt) bool {
	return p.Title == q.Title && p.Context == q.Context
}

const (
	// permissionRawRows bounds PermissionPrompt.Raw.
	permissionRawRows = 20
	// permissionContextLines bounds PermissionPrompt.Context.
	permissionContextLines = 10
	// permissionContextScanRows is how far above the title the dialog's top
	// frame rule is looked for.
	permissionContextScanRows = 40
	// permissionSelectorScanRows is how many non-blank rows above the
	// pane's bottom the last option may sit: the selector's own wrapped
	// rows plus the footer hint.
	permissionSelectorScanRows = 8
	// permissionTrailerRows is how many non-blank rows may follow the
	// selector (footer hints such as «Esc to cancel · Tab to amend»).
	permissionTrailerRows = 3
	// permissionTitleOnlyRows is how many non-blank bottom rows the
	// title-only fallback searches.
	permissionTitleOnlyRows = 12
)

// permissionOptionRow is one selector choice: «❯ 1. Yes» or «  2. No».
var permissionOptionRow = regexp.MustCompile(`^( *)(❯ *)?([0-9]+)\. +(\S.*)$`)

// permissionTitleMarkers are the question wordings the title-only fallback
// accepts. The numbered parse does not need them — it goes by structure.
var permissionTitleMarkers = []string{"Do you want to ", "Would you like to proceed?"}

// permissionFreeTextHint is the hint row Claude Code renders under a choice
// that opens a text field instead of answering («Tell Claude what to
// change» in the plan dialog). A digit key only moves the cursor there.
const permissionFreeTextHint = "shift+tab to approve"

type permissionOption struct {
	label    string
	cursor   bool
	freeText bool
}

// ParsePermissionPrompt reports whether the bottom of a pane is a Claude
// Code permission dialog waiting for a keypress, and reads it.
//
// The rule is structural: a question ending in «?» directly above a
// numbered selector («❯ 1. …», «  2. …») with exactly one cursor, sitting at
// the bottom of the pane with nothing but a short footer below it — in
// particular no composer rule, which is what tells a live dialog from the
// same text echoed in the agent's output. The AskUserQuestion widget and
// the held-peer-message dialog have the same shape and their own
// machinery, so they are excluded explicitly.
//
// A recognised question whose choices cannot be mapped to digit keys
// (unnumbered, or a free-text choice that is not the last one) still
// returns ok with empty Options. A free-text choice in last position is
// dropped from Options: digits for the remaining ones are unaffected.
func ParsePermissionPrompt(pane string) (PermissionPrompt, bool) {
	rows := visiblePaneRows(pane)
	if len(rows) == 0 {
		return PermissionPrompt{}, false
	}
	raw := strings.Join(tailRows(rows, permissionRawRows), "\n")
	if LooksLikeQuizWidget(raw) || LooksLikeHeldPeerDialog(raw) {
		return PermissionPrompt{}, false
	}
	p, ok := parseNumberedPermissionPrompt(rows)
	if !ok {
		p, ok = parseTitleOnlyPermissionPrompt(rows)
	}
	if !ok {
		return PermissionPrompt{}, false
	}
	p.Raw = raw
	return p, true
}

// parseNumberedPermissionPrompt is the structural parse: selector at the
// bottom, question right above it, context above that.
func parseNumberedPermissionPrompt(rows []string) (PermissionPrompt, bool) {
	last := -1
	nonBlank := 0
	for i := len(rows) - 1; i >= 0; i-- {
		if isFrameRule(rows[i]) {
			return PermissionPrompt{}, false
		}
		if permissionOptionRow.MatchString(rows[i]) {
			last = i
			break
		}
		if strings.TrimSpace(rows[i]) != "" {
			nonBlank++
			if nonBlank > permissionSelectorScanRows {
				return PermissionPrompt{}, false
			}
		}
	}
	if last < 0 {
		return PermissionPrompt{}, false
	}

	col := strings.IndexFunc(rows[last], func(r rune) bool { return r >= '0' && r <= '9' })
	col = utf8.RuneCountInString(rows[last][:col])

	end := last + 1
	for end < len(rows) && isOptionContinuation(rows[end], col) {
		end++
	}
	trailer := 0
	for _, r := range rows[end:] {
		if strings.TrimSpace(r) != "" {
			trailer++
		}
	}
	if trailer > permissionTrailerRows {
		return PermissionPrompt{}, false
	}

	top := last
	for top > 0 && (permissionOptionRow.MatchString(rows[top-1]) || isOptionContinuation(rows[top-1], col)) {
		top--
	}
	if !permissionOptionRow.MatchString(rows[top]) {
		return PermissionPrompt{}, false
	}

	width := maxRowWidth(rows)
	options, ok := readPermissionOptions(rows[top:end], width)
	if !ok {
		return PermissionPrompt{}, false
	}

	t := top - 1
	if t >= 0 && strings.TrimSpace(rows[t]) == "" {
		t--
	}
	if t < 0 || isFrameRule(rows[t]) || isDashedRule(rows[t]) {
		return PermissionPrompt{}, false
	}
	title := strings.TrimSpace(rows[t])
	if !strings.HasSuffix(title, "?") {
		return PermissionPrompt{}, false
	}
	for t > 0 {
		up := rows[t-1]
		if strings.TrimSpace(up) == "" || isFrameRule(up) || isDashedRule(up) {
			break
		}
		sep, wrapped := wrapSeparator(up, rows[t], width)
		if !wrapped {
			break
		}
		title = strings.TrimSpace(up) + sep + title
		t--
	}

	return PermissionPrompt{
		Title:   title,
		Context: permissionContext(rows[:t], width),
		Options: options,
	}, true
}

// readPermissionOptions folds the selector rows into choices, rejoining
// labels the TUI wrapped. It fails unless the choices are numbered 1..n
// (n ≥ 2) with exactly one cursor. Options come back nil when a free-text
// choice is anywhere but last.
func readPermissionOptions(rows []string, width int) ([]string, bool) {
	var opts []permissionOption
	prev := ""
	for _, r := range rows {
		if m := permissionOptionRow.FindStringSubmatch(r); m != nil {
			n, err := strconv.Atoi(m[3])
			if err != nil || n != len(opts)+1 {
				return nil, false
			}
			opts = append(opts, permissionOption{label: strings.TrimSpace(m[4]), cursor: m[2] != ""})
			prev = r
			continue
		}
		o := &opts[len(opts)-1]
		switch {
		case o.freeText:
			// the rest of a wrapped hint
		case strings.Contains(r, permissionFreeTextHint):
			o.freeText = true
		default:
			sep, wrapped := wrapSeparator(prev, r, width)
			if !wrapped {
				// An unexplained sub-row: treat it like a hint rather than
				// risk a digit landing in a text field.
				o.freeText = true
				break
			}
			o.label += sep + strings.TrimSpace(r)
		}
		prev = r
	}

	cursors := 0
	for _, o := range opts {
		if o.cursor {
			cursors++
		}
	}
	if len(opts) < 2 || cursors != 1 {
		return nil, false
	}

	for len(opts) > 0 && opts[len(opts)-1].freeText {
		opts = opts[:len(opts)-1]
	}
	labels := make([]string, 0, len(opts))
	for _, o := range opts {
		if o.freeText {
			return nil, true
		}
		labels = append(labels, o.label)
	}
	if len(labels) == 0 {
		return nil, true
	}
	return labels, true
}

// parseTitleOnlyPermissionPrompt is the fallback for a dialog whose
// selector is not the numbered kind: a known question wording near the
// bottom of the pane with no composer rule below it.
func parseTitleOnlyPermissionPrompt(rows []string) (PermissionPrompt, bool) {
	nonBlank := 0
	for i := len(rows) - 1; i >= 0 && nonBlank < permissionTitleOnlyRows; i-- {
		if isFrameRule(rows[i]) {
			return PermissionPrompt{}, false
		}
		t := strings.TrimSpace(rows[i])
		if t == "" {
			continue
		}
		nonBlank++
		if !strings.HasSuffix(t, "?") {
			continue
		}
		for _, marker := range permissionTitleMarkers {
			if strings.Contains(t, marker) {
				return PermissionPrompt{Title: t}, true
			}
		}
	}
	return PermissionPrompt{}, false
}

// permissionContext collects the dialog body above the title: everything up
// to the dialog's top frame rule, with wrapped rows rejoined, the dashed
// separators and «Tip:» lines dropped, and the common indent removed. The
// plan dialog has a rule directly above its title, so a rule with nothing
// collected yet is stepped over rather than taken as the top.
func permissionContext(rows []string, width int) string {
	start := len(rows)
	framed := false
	content := false
	for start > 0 && len(rows)-start < permissionContextScanRows {
		r := rows[start-1]
		if isFrameRule(r) {
			if content {
				framed = true
				break
			}
		} else if strings.TrimSpace(r) != "" && !isDashedRule(r) {
			content = true
		}
		start--
	}

	var lines []string
	prev := ""
	for _, r := range rows[start:] {
		if isFrameRule(r) || isDashedRule(r) {
			prev = ""
			continue
		}
		if prev != "" && strings.TrimSpace(r) != "" {
			if sep, wrapped := wrapSeparator(prev, r, width); wrapped {
				lines[len(lines)-1] += sep + strings.TrimSpace(r)
				prev = r
				continue
			}
		}
		lines = append(lines, r)
		prev = r
	}

	kept := lines[:0]
	for _, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "Tip:") {
			kept = append(kept, l)
		}
	}
	lines = trimBlankEnds(kept)
	lines = dedent(lines)

	if len(lines) > permissionContextLines {
		if framed {
			// The frame's top is the header (tool, path): keep it.
			lines = append(lines[:permissionContextLines:permissionContextLines], "…")
		} else {
			// No frame in sight: only the rows next to the title are
			// known to be the dialog's.
			lines = lines[len(lines)-permissionContextLines:]
		}
	}
	return strings.Join(lines, "\n")
}

// wrapSeparator reports whether next is the TUI's continuation of prev,
// wrapped at width, and what joins them: " " for a word wrap, "" for a long
// word hard-broken mid-word. It is a wrap only if next's first word could
// not have fitted at the end of prev.
func wrapSeparator(prev, next string, width int) (string, bool) {
	if width <= 0 {
		return "", false
	}
	p := strings.TrimRight(prev, " ")
	n := strings.TrimSpace(next)
	if n == "" {
		return "", false
	}
	pw := utf8.RuneCountInString(p)
	fw := utf8.RuneCountInString(firstWord(n))
	if pw+1+fw <= width {
		return "", false
	}
	indent := len(next) - len(strings.TrimLeft(next, " "))
	if pw >= width && utf8.RuneCountInString(lastWord(p))+fw > width-indent {
		return "", true
	}
	return " ", true
}

// visiblePaneRows is the pane as rows of visible text: escapes stripped,
// trailing spaces and blank padding rows trimmed, and the side borders of
// a boxed («│ … │») layout removed.
func visiblePaneRows(pane string) []string {
	parsed := trimTrailingBlankLines(parsePane(pane))
	rows := make([]string, len(parsed))
	for i, l := range parsed {
		rows[i] = stripBoxBorder(strings.TrimRight(l.text(), " \t"))
	}
	return rows
}

func stripBoxBorder(row string) string {
	t := strings.TrimLeft(row, " ")
	if len(t) > len("│") && strings.HasPrefix(t, "│") && strings.HasSuffix(t, "│") {
		return strings.TrimRight(t[len("│"):len(t)-len("│")], " ")
	}
	return row
}

// isFrameRule matches the horizontal rules that open a dialog or frame the
// composer («────…», possibly carrying a session label) and the top of a
// boxed frame («╭──…»).
func isFrameRule(row string) bool {
	t := strings.TrimSpace(row)
	return strings.HasPrefix(t, "────") || strings.HasPrefix(t, "╭")
}

// isDashedRule matches the «╌╌╌…» separators inside a dialog body.
func isDashedRule(row string) bool {
	return strings.HasPrefix(strings.TrimSpace(row), "╌╌╌╌")
}

func isOptionContinuation(row string, col int) bool {
	if strings.TrimSpace(row) == "" || permissionOptionRow.MatchString(row) {
		return false
	}
	return len(row)-len(strings.TrimLeft(row, " ")) > col
}

func maxRowWidth(rows []string) int {
	w := 0
	for _, r := range rows {
		if n := utf8.RuneCountInString(r); n > w {
			w = n
		}
	}
	return w
}

func tailRows(rows []string, n int) []string {
	if len(rows) <= n {
		return rows
	}
	return rows[len(rows)-n:]
}

func trimBlankEnds(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// dedent removes the indent common to all non-blank lines and trailing
// spaces.
func dedent(lines []string) []string {
	common := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if n := len(l) - len(strings.TrimLeft(l, " ")); common < 0 || n < common {
			common = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		out[i] = strings.TrimRight(l[common:], " ")
	}
	return out
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

func lastWord(s string) string {
	return s[strings.LastIndexByte(s, ' ')+1:]
}
