package cli

import "testing"

func TestRepoAddWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoAddCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestRepoLsWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoLsCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestRepoRmWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoRmCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}
