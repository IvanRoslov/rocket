package agentrun

import (
	"testing"
	"time"
)

func TestParseCronNext(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		after string
		want  string
	}{
		{"hourly", "0 * * * *", "2026-08-02T10:15:00Z", "2026-08-02T11:00:00Z"},
		{"every 15m", "*/15 * * * *", "2026-08-02T10:15:00Z", "2026-08-02T10:30:00Z"},
		{"weekday", "30 9 * * 1", "2026-08-02T10:15:00Z", "2026-08-03T09:30:00Z"},
		{"list", "0,45 * * * *", "2026-08-02T10:15:00Z", "2026-08-02T10:45:00Z"},
		{"range step", "0 8-18/2 * * *", "2026-08-02T10:15:00Z", "2026-08-02T12:00:00Z"},
		{"day of month", "0 0 1 * *", "2026-08-02T10:15:00Z", "2026-09-01T00:00:00Z"},
		{"sunday as 7", "0 12 * * 7", "2026-08-02T13:00:00Z", "2026-08-09T12:00:00Z"},
		{"strictly after", "0 * * * *", "2026-08-02T11:00:00Z", "2026-08-02T12:00:00Z"},
		{"drops seconds", "0 * * * *", "2026-08-02T10:59:30Z", "2026-08-02T11:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := ParseCron(tt.expr)
			if err != nil {
				t.Fatalf("ParseCron(%q): %v", tt.expr, err)
			}
			after := mustTime(t, tt.after)
			want := mustTime(t, tt.want)
			got := s.Next(after)
			if !got.Equal(want) {
				t.Errorf("Next(%s) for %q = %s, want %s", after, tt.expr, got, want)
			}
		})
	}
}

func TestParseCronRejectsGarbage(t *testing.T) {
	for _, expr := range []string{
		"", "   ", "* * * *", "* * * * * *", "61 * * * *", "a * * * *",
		"*/0 * * * *", "5-1 * * * *", "0 24 * * *", "0 0 32 * *", "0 0 * 13 *", "0 0 * * 8",
	} {
		if _, err := ParseCron(expr); err == nil {
			t.Errorf("ParseCron(%q): want error, got none", expr)
		}
	}
}

func TestScheduleNextKeepsLocation(t *testing.T) {
	s, err := ParseCron("0 * * * *")
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	loc := time.FixedZone("test", 3*3600)
	after := time.Date(2026, 8, 2, 10, 15, 0, 0, loc)
	got := s.Next(after)
	if got.Location() != loc {
		t.Fatalf("Next lost the location: %s", got.Location())
	}
	if want := time.Date(2026, 8, 2, 11, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Next = %s, want %s", got, want)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return ts.UTC()
}
