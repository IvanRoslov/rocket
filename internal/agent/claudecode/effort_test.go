package claudecode

import (
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestLaunchCommandModelAndEffort(t *testing.T) {
	base := []string{"claude", "--dangerously-skip-permissions", "--settings", sessionSettingsJSON}
	cases := []struct {
		name          string
		model, effort string
		want          []string
	}{
		{"model and effort", "opus", "high", append(append([]string{}, base...), "--model", "opus", "--effort", "high", "--", "go")},
		{"model only", "opus", "", append(append([]string{}, base...), "--model", "opus", "--", "go")},
		{"effort only", "", "max", append(append([]string{}, base...), "--effort", "max", "--", "go")},
		{"neither", "", "", append(append([]string{}, base...), "--", "go")},
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
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if got := New().Efforts(); !reflect.DeepEqual(got, want) {
		t.Errorf("Efforts = %v, want %v", got, want)
	}
}
