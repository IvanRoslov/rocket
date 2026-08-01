// Package agentrun implements the runtime of persistent agent roles: the
// wake engine (inbox debounce, single live instance per role, spawn or
// inject, run termination) and the scheduler (snooze expiry, role cron).
// The durable definitions it works on live in internal/store (agents,
// agent_inbox, agent_items); see docs/10-agents.md.
package agentrun

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed 5-field cron expression
// (minute hour day-of-month month day-of-week).
//
// rocket deliberately carries its own tiny parser rather than a cron
// dependency: role schedules are coarse ("once an hour", "every morning"),
// and the supported syntax below covers them without pulling in a library.
type Schedule struct {
	minute   [60]bool
	hour     [24]bool
	dom      [32]bool // index 1..31
	month    [13]bool // index 1..12
	dow      [7]bool  // 0 = Sunday
	domRestr bool     // day-of-month field is not "*"
	dowRestr bool     // day-of-week field is not "*"
}

// cronField describes one field's bounds for the generic parser.
type cronField struct {
	name string
	min  int
	max  int
}

// ParseCron parses a standard 5-field cron expression. Each field accepts
// "*", "*/n", a single value, "a-b", "a-b/n" and comma-separated lists of
// those. Day-of-week is 0-6 with Sunday as 0; 7 is accepted as Sunday too.
// Seconds, names ("MON"), "?" and "@daily"-style macros are not supported.
func ParseCron(expr string) (Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron %q: want 5 fields (minute hour day-of-month month day-of-week), got %d", expr, len(fields))
	}

	specs := []cronField{
		{"minute", 0, 59},
		{"hour", 0, 23},
		{"day-of-month", 1, 31},
		{"month", 1, 12},
		{"day-of-week", 0, 7},
	}

	sets := make([][]bool, 5)
	for i, spec := range specs {
		set, err := parseCronField(fields[i], spec)
		if err != nil {
			return Schedule{}, fmt.Errorf("cron %q: %w", expr, err)
		}
		sets[i] = set
	}

	var s Schedule
	copy(s.minute[:], sets[0])
	copy(s.hour[:], sets[1])
	for v := 1; v <= 31; v++ {
		s.dom[v] = sets[2][v]
	}
	for v := 1; v <= 12; v++ {
		s.month[v] = sets[3][v]
	}
	for v := 0; v <= 7; v++ {
		if sets[4][v] {
			s.dow[v%7] = true
		}
	}
	s.domRestr = strings.TrimSpace(fields[2]) != "*"
	s.dowRestr = strings.TrimSpace(fields[4]) != "*"

	return s, nil
}

// parseCronField expands one field into a bool slice indexed by value
// (length spec.max+1).
func parseCronField(field string, spec cronField) ([]bool, error) {
	set := make([]bool, spec.max+1)

	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%s: empty element in %q", spec.name, field)
		}

		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			var err error
			step, err = strconv.Atoi(part[i+1:])
			if err != nil || step < 1 {
				return nil, fmt.Errorf("%s: invalid step in %q", spec.name, part)
			}
			part = part[:i]
		}

		lo, hi := spec.min, spec.max
		switch {
		case part == "*":
			// full range, already set
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			var err error
			if lo, err = parseCronValue(bounds[0], spec); err != nil {
				return nil, err
			}
			if hi, err = parseCronValue(bounds[1], spec); err != nil {
				return nil, err
			}
			if lo > hi {
				return nil, fmt.Errorf("%s: reversed range %q", spec.name, part)
			}
		default:
			v, err := parseCronValue(part, spec)
			if err != nil {
				return nil, err
			}
			lo, hi = v, v
		}

		for v := lo; v <= hi; v += step {
			set[v] = true
		}
	}

	return set, nil
}

func parseCronValue(s string, spec cronField) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", spec.name, s)
	}
	if v < spec.min || v > spec.max {
		return 0, fmt.Errorf("%s: %d out of range %d-%d", spec.name, v, spec.min, spec.max)
	}
	return v, nil
}

// maxCronLookahead bounds the minute-by-minute scan in Next. A year covers
// every expression this parser can express; anything beyond it (e.g. a
// February 30th) has no next occurrence at all.
const maxCronLookahead = 366 * 24 * 60

// Next returns the first time strictly after `after` that matches the
// schedule, in after's own location. It returns the zero time if the
// expression can never match (e.g. "0 0 30 2 *").
func (s Schedule) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute)
	if !t.After(after) {
		// Truncate rounds down, so this is the normal path: start scanning
		// from the next whole minute.
		t = t.Add(time.Minute)
	}

	for i := 0; i < maxCronLookahead; i++ {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

// matches reports whether t (whole minutes) satisfies the schedule. When
// both day-of-month and day-of-week are restricted, cron's traditional rule
// applies: the day matches if EITHER field matches.
func (s Schedule) matches(t time.Time) bool {
	if !s.minute[t.Minute()] || !s.hour[t.Hour()] || !s.month[int(t.Month())] {
		return false
	}

	domOK := s.dom[t.Day()]
	dowOK := s.dow[int(t.Weekday())]
	switch {
	case s.domRestr && s.dowRestr:
		return domOK || dowOK
	case s.domRestr:
		return domOK
	case s.dowRestr:
		return dowOK
	default:
		return true
	}
}
