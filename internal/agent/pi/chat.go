package pi

import (
	"context"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func (p *Pi) TranscriptTail(context.Context, agent.ActivityRef, string) ([]agent.ChatEntry, string, error) {
	return nil, "", agent.ErrNoSignal
}

func (p *Pi) TranscriptStat(context.Context, agent.ActivityRef) (int64, int64, error) {
	return 0, 0, agent.ErrNoSignal
}
