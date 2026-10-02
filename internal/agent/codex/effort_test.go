package codex

import (
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestLaunchCommandModelAndEffort(t *testing.T) {
	cases := []struct {
		name          string
		model, effort string
		want          []string
	}{
		{"model and effort", "gpt-5", "high",
			[]string{"codex", "--no-alt-screen", "--sandbox", "danger-full-access", "--ask-for-approval", "never",
				"-m", "gpt-5", "-c", "model_reasoning_effort=high", "--", "go"}},
		{"model only", "gpt-5", "",
			[]string{"codex", "--no-alt-screen", "--sandbox", "danger-full-access", "--ask-for-approval", "never",
				"-m", "gpt-5", "--", "go"}},
		{"effort only", "", "low",
			[]string{"codex", "--no-alt-screen", "--sandbox", "danger-full-access", "--ask-for-approval", "never",
				"-c", "model_reasoning_effort=low", "--", "go"}},
		{"neither", "", "",
			[]string{"codex", "--no-alt-screen", "--sandbox", "danger-full-access", "--ask-for-approval", "never", "--", "go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := New().LaunchCommand(agent.LaunchSpec{Model: tc.model, Effort: tc.effort, FirstMessage: "go"})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("LaunchCommand = %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestEfforts(t *testing.T) {
	want := []string{"minimal", "low", "medium", "high", "xhigh", "max", "ultra"}
	if got := New().Efforts(); !reflect.DeepEqual(got, want) {
		t.Errorf("Efforts = %v, want %v", got, want)
	}
}
