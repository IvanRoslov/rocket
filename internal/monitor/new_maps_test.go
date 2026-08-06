package monitor

import "testing"

// New must initialise every map the sweep writes to. The unit tests build
// Monitor by hand, so a map missing from New goes unnoticed there and only
// panics in the daemon ("assignment to entry in nil map") on the first sweep
// that touches it.
func TestNewInitialisesWritableMaps(t *testing.T) {
	m := New(nil, nil, nil, nil, nil)

	maps := []struct {
		name  string
		write func()
	}{
		{"push", func() { m.push["s"] = pushEntry{} }},
		{"cache", func() { m.cache["s"] = "" }},
		{"chat", func() { m.chat["s"] = chatStat{} }},
		{"quizMiss", func() { m.quizMiss["s"]++ }},
		{"inputWaitMiss", func() { m.inputWaitMiss["s"]++ }},
	}

	for _, mp := range maps {
		t.Run(mp.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("New() left %s nil: writing to it panics: %v", mp.name, r)
				}
			}()
			mp.write()
		})
	}
}
