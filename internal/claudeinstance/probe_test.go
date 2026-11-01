package claudeinstance

import (
	"testing"
	"time"
)

func TestParseProcessMetadata(t *testing.T) {
	p, err := parseProcess("11 10 Sun Nov  1 03:00:00 2026 /usr/local/bin/claude\n")
	if err != nil || p.PID != 11 || p.PPID != 10 || p.Name != "claude" || !p.Start.Equal(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", p, err)
	}
	for _, s := range []string{"", "11 10 bad claude", "11 10 Sun Nov 1 03:00:00 2026 claude extra\nforeign"} {
		if _, err := parseProcess(s); err == nil {
			t.Fatalf("accepted malformed %q", s)
		}
	}
}
