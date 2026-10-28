package runtime

import (
	"errors"
	"strings"
)

// ErrQueuedByAgent is returned by Inject when the agent took the text into
// its own pending queue instead of submitting it right away: Codex, busy
// with a turn, shows it under «Messages to be submitted after next tool
// call» and hands it to the model later on its own. Enter was pressed once
// and nothing was cleared. The text is no longer in the composer, so
// re-injecting would queue a duplicate; the caller must confirm delivery
// out of band (agent.DeliveryConfirmer) rather than retry.
var ErrQueuedByAgent = errors.New("inject: text accepted into the agent's own pending queue")

// codexQueueHeader opens Codex's pending-queue block above its composer.
const codexQueueHeader = "Messages to be submitted after next tool call"

// codexComposerMark starts Codex's composer row (and its history echoes).
const codexComposerMark = "›"

// queueFooterRows is how many non-blank rows may follow the composer row for
// the block above it to count as the live bottom of the screen (model line,
// hints) rather than something in history.
const queueFooterRows = 4

// minQueuedPrefix is the shortest visible start of a truncated queue item
// (in non-space characters) that LooksLikeAgentQueued accepts as our text.
const minQueuedPrefix = 20

// LooksLikeAgentQueued reports whether pane shows the agent's own pending
// queue at the bottom of the screen holding marker — the injected text, or
// a logical line of it. Today only Codex has such a queue:
//
//	• Messages to be submitted after next tool call (press esc to interrupt and send immediately)
//	  ↳ [from probe] MARKER-ALPHA please reply ACK-ALPHA
//	› Ask Codex to do anything
//
// The block must be live: the last header in pane, followed by its ↳ rows
// and then the composer row near the bottom. The same text echoed in
// history, or still sitting in the composer, does not count.
//
// Codex wraps a long item over indented rows and cuts an item after a few
// rows with a «…» row; it also appends a message queued behind another to
// that same item on a new row. So marker matches when the item holds it
// whole (ignoring whitespace, like ContainsMarker), or when a truncated
// item ends with a visible start of marker beginning at a row start.
func LooksLikeAgentQueued(pane, marker string) bool {
	want := stripWhitespace(marker)
	if want == "" {
		return false
	}
	lines := strings.Split(trimTrailingBlank(pane), "\n")

	header := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], codexQueueHeader) {
			header = i
			break
		}
	}
	if header < 0 {
		return false
	}

	composer := -1
	for i := header + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), codexComposerMark) {
			composer = i
			break
		}
	}
	if composer < 0 || nonBlankLineCount(strings.Join(lines[composer+1:], "\n")) > queueFooterRows {
		return false
	}

	// The text must have left the composer: an identical older item in the
	// block says nothing about a copy that Enter failed to submit.
	head := []rune(want)
	if len(head) > minQueuedPrefix {
		head = head[:minQueuedPrefix]
	}
	if strings.Contains(stripWhitespace(strings.Join(lines[composer:], "\n")), string(head)) {
		return false
	}

	for _, item := range queueItems(lines[header+1 : composer]) {
		if item.matches(want) {
			return true
		}
	}
	return false
}

// queueItem is one ↳ entry of the queue block: its visible rows (marker and
// indentation removed) and whether Codex cut it short with «…».
type queueItem struct {
	rows      []string
	truncated bool
}

func (q queueItem) matches(want string) bool {
	if strings.Contains(stripWhitespace(strings.Join(q.rows, "\n")), want) {
		return true
	}
	if !q.truncated {
		return false
	}
	for j := range q.rows {
		frag := stripWhitespace(strings.Join(q.rows[j:], "\n"))
		if len([]rune(frag)) >= minQueuedPrefix && strings.HasPrefix(want, frag) {
			return true
		}
	}
	return false
}

// queueItems splits the rows between the queue header and the composer into
// ↳ items.
func queueItems(rows []string) []queueItem {
	var items []queueItem
	for _, row := range rows {
		s := strings.TrimSpace(row)
		if s == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(s, "↳"); ok {
			items = append(items, queueItem{})
			s = strings.TrimSpace(rest)
		}
		if len(items) == 0 {
			continue
		}
		cur := &items[len(items)-1]
		if cur.truncated {
			continue
		}
		if s == "…" {
			cur.truncated = true
			continue
		}
		if cut, ok := strings.CutSuffix(s, "…"); ok {
			cur.truncated = true
			s = cut
		}
		cur.rows = append(cur.rows, s)
	}
	return items
}
