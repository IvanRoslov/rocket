package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestReadTextInputFromFile reads a body off disk verbatim: markdown with
// backticks and newlines must survive, which is the whole point of --file.
func TestReadTextInputFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	content := "line one\n\n```go\nfmt.Println(\"hi\")\n```\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := readTextInput(&cobra.Command{}, path)
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

// TestReadTextInputFromStdin checks that "-" reads the command's stdin, so
// `echo ... | rocket task log 1 --kind note --file -` works.
func TestReadTextInputFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("piped `body`\n"))

	got, err := readTextInput(cmd, stdinMarker)
	if err != nil {
		t.Fatalf("readTextInput: %v", err)
	}
	if got != "piped `body`\n" {
		t.Errorf("got %q, want %q", got, "piped `body`\n")
	}
}

// TestReadTextInputMissingFile reports a missing file rather than silently
// sending an empty body.
func TestReadTextInputMissingFile(t *testing.T) {
	if _, err := readTextInput(&cobra.Command{}, filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

// TestTextBodySources pins the exactly-one-of contract: the positional text
// or --file, never both and never neither. Both/neither must be a usage
// error (exit code 3), not a silent preference that could drop the user's
// markdown on a typo.
func TestTextBodySources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte("from file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name     string
		arg      string
		hasArg   bool
		filePath string
		want     string
		wantExit int
	}{
		{name: "positional only", arg: "from arg", hasArg: true, want: "from arg"},
		{name: "file only", filePath: path, want: "from file"},
		{name: "both", arg: "from arg", hasArg: true, filePath: path, wantExit: 3},
		{name: "neither", wantExit: 3},
		{name: "empty positional counts as given", arg: "", hasArg: true, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := textBody(&cobra.Command{}, tc.arg, tc.hasArg, tc.filePath, "usage: test")
			if tc.wantExit != 0 {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if code := exitCode(err); code != tc.wantExit {
					t.Fatalf("exitCode = %d, want %d (err=%v)", code, tc.wantExit, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestArgAt covers the bounds helper the writing commands use to tell an
// absent positional argument from an empty one.
func TestArgAt(t *testing.T) {
	args := []string{"id", "text"}
	if got := argAt(args, 1); got != "text" {
		t.Errorf("argAt(args, 1) = %q, want %q", got, "text")
	}
	if got := argAt(args[:1], 1); got != "" {
		t.Errorf("argAt(short, 1) = %q, want %q", got, "")
	}
}
