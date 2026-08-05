package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootHelpListsCoreCommands(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"daemon", "--json", "--socket"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}

// TestRootRoutesResultsToStdout pins the pipe contract: a command's result
// goes to stdout, so `rocket ... | grep` and `... | jq` work. cobra's
// Print/Printf/Println write to OutOrStderr(), which is stderr until an out
// writer is set on the command tree — so without an explicit SetOut on the
// root every result line lands on stderr. That is the bug this guards
// against (confirmed against the live binary: `rocket send <s> hi
// 1>/dev/null` still printed "message N queued").
func TestRootRoutesResultsToStdout(t *testing.T) {
	// Deliberately NOT calling SetOut first: the point is that NewRootCmd
	// already installed a stdout writer. Reach for it through the very API
	// cobra's Print* family uses.
	if got := NewRootCmd().OutOrStderr(); got != os.Stdout {
		t.Fatalf("root.OutOrStderr() = %v, want os.Stdout", got)
	}

	// And once a test overrides the writer, Print* must honour the override
	// rather than a hardcoded os.Stdout — otherwise no command stays
	// testable.
	root := NewRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("result line")
			cmd.PrintErrln("diagnostic line")
			return nil
		},
	})
	root.SetArgs([]string{"probe"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !strings.Contains(out.String(), "result line") {
		t.Errorf("stdout = %q, want it to contain %q", out.String(), "result line")
	}
	if strings.Contains(out.String(), "diagnostic line") {
		t.Errorf("stdout = %q, must not contain the diagnostic line", out.String())
	}
	if !strings.Contains(errBuf.String(), "diagnostic line") {
		t.Errorf("stderr = %q, want it to contain %q", errBuf.String(), "diagnostic line")
	}
}
