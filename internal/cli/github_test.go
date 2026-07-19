package cli

import "testing"

func TestGithubAuthWrongArgCountIsUsageError(t *testing.T) {
	cases := [][]string{
		{},
		{"tok1", "tok2"},
	}
	for _, args := range cases {
		cmd := newGithubAuthCmd()
		cmd.SilenceUsage = true
		cmd.SetArgs(args)
		err := cmd.Execute()
		if exitCode(err) != 3 {
			t.Fatalf("args=%v: exitCode = %d, want 3 (err=%v)", args, exitCode(err), err)
		}
	}
}
