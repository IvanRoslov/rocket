package pi

import (
	"context"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

func (p *Pi) Activity(context.Context, agent.ActivityRef) (activity.State, time.Time, error) {
	return "", time.Time{}, agent.ErrNoSignal
}
