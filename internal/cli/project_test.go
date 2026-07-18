package cli

import "testing"

func TestProjectCreateWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectCreateCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectCreateMissingMainIsUsageError(t *testing.T) {
	cmd := newProjectCreateCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"myproj"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectLsWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectLsCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectShowWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectShowCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectLinkWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectLinkCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"onlyone"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectUnlinkWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectUnlinkCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestProjectRmWrongArgCountIsUsageError(t *testing.T) {
	cmd := newProjectRmCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}
