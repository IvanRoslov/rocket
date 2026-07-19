package codex

import (
	"context"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// TranscriptTail is a stub for now: codex's session JSONL taxonomy for chat
// content (response_item/event_msg) has not been parsed yet (tracked as a
// follow-up task). Always returns agent.ErrNoSignal so callers see "no chat
// available" rather than a wrong result.
func (c *Codex) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	return nil, "", agent.ErrNoSignal
}

// TranscriptStat is a stub for now, matching TranscriptTail — see its
// comment.
func (c *Codex) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	return 0, 0, agent.ErrNoSignal
}
