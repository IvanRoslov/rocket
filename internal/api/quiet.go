package api

import (
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/heartbeat"
)

// quietMilestoneSet is the set of milestone ids currently showing no trace of
// the agent holding them. Like waitingTerminal, the flag it feeds is derived
// on every read and never persisted: it is a function of the milestone's
// activity and the clock, so storing it would only give us a stale copy.
type quietMilestoneSet map[int64]bool

// milestoneQuietAfter returns the configured quiet threshold, falling back to
// the built-in default when Deps carries no (or a zero) config.
func milestoneQuietAfter(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.MilestoneQuietAfter > 0 {
		return cfg.MilestoneQuietAfter
	}
	return config.DefaultMilestoneQuietAfter
}

// quietMilestones computes the quiet set over every milestone being worked on,
// in a query per active status plus one — the same rule the heartbeat reminds by (heartbeat.QuietMilestone),
// so a badge and a reminder can never disagree. An installation with no
// milestone in progress costs one query and stops there.
func quietMilestones(d Deps) (quietMilestoneSet, error) {
	tasks, err := heartbeat.ActiveMilestones(d.Store)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return quietMilestoneSet{}, nil
	}
	activity, err := d.Store.MilestoneActivity()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	after := milestoneQuietAfter(d.Cfg)
	out := make(quietMilestoneSet, len(tasks))
	for _, t := range tasks {
		if _, quiet := heartbeat.QuietMilestone(t, activity[t.ID], now, after); quiet {
			out[t.ID] = true
		}
	}
	return out, nil
}

// annotateQuiet flags tr when its milestone is in the quiet set.
func annotateQuiet(tr *taskResponse, quiet quietMilestoneSet) {
	tr.Quiet = quiet[tr.ID]
}
