package kimicode

import (
	"context"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var _ agent.StartupProber = (*KimiCode)(nil)

// HasStartedTurn implements agent.StartupProber: Kimi got past its start
// screens once its journal holds a user-origin turn.prompt at or after
// ref.Since. No journal yet is ErrNoSignal.
func (k *KimiCode) HasStartedTurn(ctx context.Context, ref agent.ActivityRef) (bool, error) {
	dir, err := findSession(kimiHome(), ref.WorktreePath, ref.Since)
	if err != nil {
		return false, agent.ErrNoSignal
	}
	started := false
	_, err = readTail(mainWire(dir), func(rec wireRecord) bool {
		if !ref.Since.IsZero() && hasTime(rec) && rec.Time.Before(ref.Since) {
			return true
		}
		if _, ok := userPrompt(rec); ok {
			started = true
			return true
		}
		return ctx.Err() != nil
	})
	if err != nil {
		return false, agent.ErrNoSignal
	}
	return started, ctx.Err()
}
