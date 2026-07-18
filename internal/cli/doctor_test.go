package cli

import (
	"bytes"
	"testing"
)

func TestParseTmuxVersion(t *testing.T) {
	cases := []struct {
		in        string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{"tmux 3.6a\n", 3, 6, true},
		{"tmux 3.0\n", 3, 0, true},
		{"tmux 2.8\n", 2, 8, true},
		{"tmux next-3.4\n", 3, 4, true},
		{"garbage output\n", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := parseTmuxVersion(c.in)
		if ok != c.wantOK {
			t.Errorf("parseTmuxVersion(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if major != c.wantMajor || minor != c.wantMinor {
			t.Errorf("parseTmuxVersion(%q) = %d.%d, want %d.%d", c.in, major, minor, c.wantMajor, c.wantMinor)
		}
	}
}

func TestPrintDoctorResults_AnyFail(t *testing.T) {
	var buf bytes.Buffer
	results := []checkResult{
		{statusOK, "a", "ok"},
		{statusWarn, "b", "meh"},
	}
	if printDoctorResults(&buf, results) {
		t.Fatal("expected anyFail = false")
	}

	results = append(results, checkResult{statusFail, "c", "bad"})
	if !printDoctorResults(&buf, results) {
		t.Fatal("expected anyFail = true")
	}
}
