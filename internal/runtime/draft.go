package runtime

import (
	"strings"
	"unicode"
)

// draftWindow is how many bottom rows of a pane LooksLikeUserDraft
// examines. The composer is the last thing rendered above the agent's
// footer chrome, so a narrow window is both enough and safer: it cannot be
// fooled by a composer-shaped line that scrolled past earlier in the
// agent's own output (a quoted "> …" line, a list of queued messages).
const draftWindow = 8

// draftPlaceholders are literal beginnings of text an agent renders INSIDE
// its composer when the composer is in fact empty — hints, not something a
// human typed. Deliberately literal and narrow, exactly like
// inputWaitMarkers: a generic "does this look like typing?" heuristic over
// TUI output is what the rule in docs/07-activity.md warns against.
var draftPlaceholders = []string{
	"Press up to edit queued messages",
	`Try "`,
	"? for shortcuts",
}

// draftCursorGlyphs are block/bar glyphs a TUI may paint as its cursor
// inside an otherwise empty composer.
const draftCursorGlyphs = "█▉▊▋▌▍▎▏▁▂▃"

// draftRuleGlyphs are the box-drawing runes a composer's surrounding rules
// and borders are made of. A line consisting solely of these closes the
// composer region.
const draftRuleGlyphs = "─━-╌═╭╮╰╯└┘┌┐│  "

// LooksLikeUserDraft reports whether the bottom of a pane shows a composer
// holding text a human typed but has not submitted yet — i.e. text that
// Inject's pre-paste C-u would silently erase.
//
// pane may be captured with or without escape sequences. Captured WITH them
// (`capture-pane -p -e`, what the guard's call sites use) the answer is
// strictly better, because the dim attribute separates typed text from
// Claude Code's ghost-text autosuggestion, which is otherwise byte-identical
// to a draft; captured without them every rune counts as typed, which is the
// behaviour this function had before it learned about attributes.
//
// Recognized composer shapes (verified against live panes, 2026-08-10):
//
//   - Claude Code, rule style — a "❯ " line at column 0 sandwiched between
//     two horizontal rules:
//     ────────────────
//     ❯ drop the deprecated always-auth key
//     ────────────────
//     ⏵⏵ bypass permissions on …
//   - Claude Code, boxed style — "│ > … │" inside a ╭─…╯ box.
//
// Everything else — notably codex, whose composer rendering has not been
// verified — is deliberately NOT recognized: adding a shape here is how
// support for another agent gets extended. A bare "> text" at column 0 is
// also not recognized; it is indistinguishable from ordinary agent output.
//
// The answer is only ever acted on in the conservative direction: a true
// answer defers a delivery (recoverable — the queue retries, and the
// busy-deadline forces the delivery through eventually), while an
// unrecognized rendering leaves today's behavior exactly as it was. This
// function must therefore prefer false whenever it cannot positively
// identify a composer: no closing rule/border, no prompt line, or content
// that matches a known placeholder.
func LooksLikeUserDraft(pane string) bool {
	lines := tailPaneLines(trimTrailingBlankLines(parsePane(pane)), draftWindow)

	// The composer is the LAST prompt line in the window: earlier matches
	// are history (e.g. Claude Code lists queued messages as "  ❯ …" above
	// the composer, indented — those are excluded by the column-0 rule
	// anyway, but the last-match rule keeps this robust).
	start := -1
	boxed := false
	for i, line := range lines {
		if b, ok := composerPromptLine(line.text()); ok {
			start, boxed = i, b
		}
	}
	if start < 0 {
		return false
	}

	content, _ := composerContent(lines[start], boxed)
	closed := false
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if isComposerRule(line.text()) {
			closed = true
			break
		}
		if _, ok := composerPromptLine(line.text()); ok {
			// A second prompt line right below: not a shape we understand.
			break
		}
		rest, ok := composerContent(line, boxed)
		if !ok {
			break
		}
		content += " " + rest
	}
	if !closed {
		// No closing rule/border: the prompt-looking line was not a
		// composer we can reason about. Stay conservative.
		return false
	}

	content = strings.TrimSpace(strings.Trim(strings.TrimSpace(content), draftCursorGlyphs))
	if content == "" {
		return false
	}
	for _, p := range draftPlaceholders {
		if strings.HasPrefix(content, p) {
			return false
		}
	}
	return true
}

// composerPromptLine reports whether line is a composer's prompt line, and
// whether it is the boxed variant ("│ > …") rather than the rule variant
// ("❯ …" at column 0).
func composerPromptLine(line string) (boxed, ok bool) {
	if rest, found := strings.CutPrefix(line, "❯"); found {
		return false, startsWithSpace(rest)
	}
	if rest, found := strings.CutPrefix(line, "│"); found {
		rest = trimLeftSpace(rest)
		if after, found := strings.CutPrefix(rest, ">"); found {
			return true, startsWithSpace(after)
		}
	}
	return false, false
}

// startsWithSpace reports whether s is empty or begins with a whitespace
// rune. It is not strings.HasPrefix(s, " ") because Claude Code separates
// its prompt sigil from the draft with a NO-BREAK SPACE (U+00A0), not an
// ASCII one — a real capture that silently defeated the ASCII check.
func startsWithSpace(s string) bool {
	if s == "" {
		return true
	}
	r := []rune(s)[0]
	return unicode.IsSpace(r)
}

// trimLeftSpace drops leading whitespace of any kind (see startsWithSpace).
func trimLeftSpace(s string) string {
	return strings.TrimLeftFunc(s, unicode.IsSpace)
}

// composerContent strips a composer line's chrome — prompt sigil, box
// borders — and returns the bare text on it, with dim runes dropped (see
// paneLine.normalIntensity). ok is false for a line that does not belong to
// the composer region at all (in the boxed style, one that has no left
// border), which ends the region.
//
// Chrome is matched on the FULL visible text, dim or not: a TUI is free to
// paint its rules, borders and prompt sigil dim (Claude Code paints them in
// a grey 256-colour), and losing them would make every composer
// unrecognisable — i.e. would silently disable the guard.
func composerContent(l paneLine, boxed bool) (text string, ok bool) {
	from, to := 0, len(l.runes)
	if boxed {
		if to == 0 || l.runes[0] != '│' {
			return "", false
		}
		from = 1
		for to > from && unicode.IsSpace(l.runes[to-1]) {
			to--
		}
		if to > from && l.runes[to-1] == '│' {
			to--
		}
		for from < to && unicode.IsSpace(l.runes[from]) {
			from++
		}
		if from < to && l.runes[from] == '>' {
			from++
		}
		return l.normalIntensity(from, to), true
	}
	if to > 0 && l.runes[0] == '❯' {
		return l.normalIntensity(1, to), true
	}
	// A continuation row of a multi-line draft: indented plain text. A
	// blank row ends the region (an empty composer padded to two rows).
	if strings.TrimSpace(l.text()) == "" {
		return "", false
	}
	return l.normalIntensity(0, to), true
}

// paneLine is one captured pane row: its visible runes, plus for each rune
// whether it was painted dim (SGR 2). A pane captured without `-e` carries
// no attributes at all and therefore has an all-false mask, which makes
// every rune count as normal intensity — exactly the pre-escape behaviour.
type paneLine struct {
	runes []rune
	dim   []bool
}

func (l paneLine) text() string { return string(l.runes) }

// normalIntensity returns the runes of l[from:to) that are NOT dim,
// space-trimmed. Dim runes are dropped rather than kept because that is the
// signal this whole file turns on: Claude Code renders its ghost-text
// autosuggestion — a message it predicts, that the human never typed —
// entirely inside an SGR-2 span, while real keystrokes come back at normal
// intensity. A composer holding only a suggestion therefore yields "" and
// is not a draft; one holding any typed rune keeps it and is.
func (l paneLine) normalIntensity(from, to int) string {
	var b strings.Builder
	for i := from; i < to && i < len(l.runes); i++ {
		if l.dim[i] {
			continue
		}
		b.WriteRune(l.runes[i])
	}
	return strings.TrimSpace(b.String())
}

// parsePane splits a captured pane into rows of visible runes, consuming
// the ANSI escape sequences `tmux capture-pane -e` emits and tracking the
// only attribute this file cares about: SGR 2 (dim/faint), turned off by
// SGR 22 and by the SGR 0 reset. Every other sequence — colours, OSC 8
// hyperlinks, cursor moves — is dropped, contributing no visible runes.
func parsePane(pane string) []paneLine {
	var lines []paneLine
	for _, raw := range strings.Split(pane, "\n") {
		src := []rune(raw)
		line := paneLine{}
		dim := false
		for i := 0; i < len(src); {
			if src[i] != 0x1b {
				line.runes = append(line.runes, src[i])
				line.dim = append(line.dim, dim)
				i++
				continue
			}
			n, sgr, isSGR := scanEscape(src[i:])
			if isSGR {
				dim = applySGR(dim, sgr)
			}
			i += n
		}
		lines = append(lines, line)
	}
	return lines
}

// scanEscape measures the escape sequence starting at src[0] (which is ESC)
// and, for a CSI sequence ending in 'm', returns its parameter body so the
// caller can read the SGR codes out of it. A malformed or truncated
// sequence consumes the rest of the input: half an escape sequence has no
// visible text in it either way.
func scanEscape(src []rune) (n int, params string, isSGR bool) {
	if len(src) < 2 {
		return len(src), "", false
	}
	switch src[1] {
	case '[': // CSI: ESC [ params final
		for i := 2; i < len(src); i++ {
			if src[i] >= 0x40 && src[i] <= 0x7e {
				return i + 1, string(src[2:i]), src[i] == 'm'
			}
		}
		return len(src), "", false
	case ']': // OSC: ESC ] payload (BEL | ESC \)
		for i := 2; i < len(src); i++ {
			if src[i] == 0x07 {
				return i + 1, "", false
			}
			if src[i] == 0x1b && i+1 < len(src) && src[i+1] == '\\' {
				return i + 2, "", false
			}
		}
		return len(src), "", false
	default: // two-rune escape (ESC \, ESC =, …)
		return 2, "", false
	}
}

// applySGR folds one SGR parameter body into the running dim state. Codes
// other than 2 (dim on), 22 (normal intensity) and 0 (reset everything)
// leave it alone; an empty body means 0.
func applySGR(dim bool, params string) bool {
	if params == "" {
		return false
	}
	for _, p := range strings.Split(params, ";") {
		// Sub-parameters (38:2:…) never carry an intensity code.
		if idx := strings.IndexByte(p, ':'); idx >= 0 {
			p = p[:idx]
		}
		switch strings.TrimLeft(p, "0") {
		case "": // "0", "00", "" — reset
			dim = false
		case "2":
			dim = true
		case "22":
			dim = false
		}
	}
	return dim
}

// trimTrailingBlankLines is trimTrailingBlank over parsed rows: a row that
// carried only escape sequences has no visible text and counts as blank
// padding, which a byte-level check on the raw capture would miss.
func trimTrailingBlankLines(lines []paneLine) []paneLine {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1].text()) == "" {
		end--
	}
	return lines[:end]
}

// tailVisibleLines is tailLines+trimTrailingBlank for a capture that still
// carries escape sequences: it drops trailing rows with no VISIBLE text and
// returns the last n rows unchanged — raw bytes, attributes and all.
func tailVisibleLines(pane string, n int) string {
	raw := strings.Split(pane, "\n")
	parsed := parsePane(pane)
	end := len(raw)
	for end > 0 && strings.TrimSpace(parsed[end-1].text()) == "" {
		end--
	}
	raw = raw[:end]
	if len(raw) > n {
		raw = raw[len(raw)-n:]
	}
	return strings.Join(raw, "\n")
}

// tailPaneLines is tailLines over parsed rows.
func tailPaneLines(lines []paneLine, n int) []paneLine {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// isComposerRule reports whether line is one of the horizontal rules (or a
// box's top/bottom border) that bracket the composer.
func isComposerRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len([]rune(trimmed)) < 3 {
		return false
	}
	return strings.IndexFunc(trimmed, func(r rune) bool {
		return !strings.ContainsRune(draftRuleGlyphs, r)
	}) < 0
}
