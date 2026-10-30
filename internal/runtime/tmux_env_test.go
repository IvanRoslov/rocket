package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTmuxUnsetEnvScrubsInheritedAndMergedValues(t *testing.T) {
	requireTmux(t)
	ctx := context.Background()
	rt := NewTmux()
	t.Setenv("ROCKET_TEST_DAEMON_KEY", "daemon-secret")

	dir := t.TempDir()
	name := uniqueName(t, "-env-scrub")
	h, err := rt.Create(ctx, CreateSpec{
		Name:    name,
		Dir:     dir,
		Command: `printf 'daemon=%s\nrepo=%s\nselected=%s\n' "${ROCKET_TEST_DAEMON_KEY-unset}" "${ROCKET_TEST_REPO_KEY-unset}" "$ROCKET_TEST_SELECTED_KEY"; exec cat`,
		Env: map[string]string{
			"ROCKET_TEST_REPO_KEY":     "repo-secret",
			"ROCKET_TEST_SELECTED_KEY": "selected-secret",
		},
		UnsetEnv: []string{"ROCKET_TEST_DAEMON_KEY", "ROCKET_TEST_REPO_KEY"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer rt.Destroy(ctx, h)

	waitFor(t, func() bool {
		out, err := rt.Capture(ctx, h, 20)
		return err == nil && strings.Contains(out, "daemon=unset") &&
			strings.Contains(out, "repo=unset") && strings.Contains(out, "selected=selected-secret")
	}, "launch shell to scrub unrelated keys and retain the selected key")

	script, err := os.ReadFile(filepath.Join(dir, ".rocket-launch.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"daemon-secret", "repo-secret", "selected-secret"} {
		if strings.Contains(string(script), value) {
			t.Errorf("launch script persisted credential value %q", value)
		}
	}
}
