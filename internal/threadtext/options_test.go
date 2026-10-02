package threadtext

import "testing"

func TestOptionsLine(t *testing.T) {
	cases := []struct {
		opts []string
		rec  int
		want string
	}{
		{nil, 0, ""},
		{[]string{"A", "B"}, 0, "варианты: 1) A  2) B"},
		{[]string{"A", "B"}, 2, "варианты: 1) A  2) B ★ рекомендовано"},
		{[]string{"A", "B"}, 5, "варианты: 1) A  2) B"},
		{[]string{"A", "B"}, -1, "варианты: 1) A  2) B"},
	}
	for _, c := range cases {
		if got := OptionsLine(c.opts, c.rec); got != c.want {
			t.Errorf("OptionsLine(%v,%d) = %q, want %q", c.opts, c.rec, got, c.want)
		}
	}
}
