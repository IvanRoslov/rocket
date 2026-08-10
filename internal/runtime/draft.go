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
	lines := strings.Split(tailLines(trimTrailingBlank(pane), draftWindow), "\n")

	// The composer is the LAST prompt line in the window: earlier matches
	// are history (e.g. Claude Code lists queued messages as "  ❯ …" above
	// the composer, indented — those are excluded by the column-0 rule
	// anyway, but the last-match rule keeps this robust).
	start := -1
	boxed := false
	for i, line := range lines {
		if b, ok := composerPromptLine(line); ok {
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
		if isComposerRule(line) {
			closed = true
			break
		}
		if _, ok := composerPromptLine(line); ok {
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
// borders — and returns the bare text on it. ok is false for a line that
// does not belong to the composer region at all (in the boxed style, one
// that has no left border), which ends the region.
func composerContent(line string, boxed bool) (text string, ok bool) {
	if boxed {
		rest, found := strings.CutPrefix(line, "│")
		if !found {
			return "", false
		}
		rest = strings.TrimSuffix(strings.TrimRightFunc(rest, unicode.IsSpace), "│")
		rest = trimLeftSpace(rest)
		if after, found := strings.CutPrefix(rest, ">"); found {
			rest = after
		}
		return strings.TrimSpace(rest), true
	}
	if rest, found := strings.CutPrefix(line, "❯"); found {
		return strings.TrimSpace(rest), true
	}
	// A continuation row of a multi-line draft: indented plain text. A
	// blank row ends the region (an empty composer padded to two rows).
	if strings.TrimSpace(line) == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
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
