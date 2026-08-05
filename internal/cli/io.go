package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// stdinMarker is the conventional --file value meaning "read the body from
// stdin". It exists because the shell is the enemy here: a markdown body
// passed as a positional argument gets its backticks run as command
// substitution and its $words expanded, so an agent trying to send a code
// block loses exactly the content it was quoting. A pipe (or a file) never
// reaches the shell's parser.
const stdinMarker = "-"

// readTextInput reads a text body from path, treating stdinMarker as the
// command's stdin. Content is returned verbatim — no trimming — because
// trailing newlines are part of markdown and the caller may be sending a
// whole document.
func readTextInput(cmd *cobra.Command, path string) (string, error) {
	if path == stdinMarker {
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// textBody resolves the body of a text-writing command from exactly one of
// two sources: the positional text argument or --file (with "-" for stdin).
// hasArg says whether the positional argument was supplied at all, which is
// what distinguishes "the user passed an empty string" from "the user
// passed nothing".
//
// Both sources at once, or neither, is a usage error carrying the command's
// own usage line, so it maps to exit code 3 like every other misuse.
// Silently preferring one source over the other would let a typo drop the
// user's markdown without a word.
func textBody(cmd *cobra.Command, arg string, hasArg bool, filePath, usage string) (string, error) {
	if hasArg == (filePath != "") {
		return "", &usageError{message: usage}
	}
	if filePath != "" {
		return readTextInput(cmd, filePath)
	}
	return arg, nil
}

// argAt returns args[i], or "" when the slice is shorter. Callers pair it
// with their own len(args) check to tell "absent" from "empty".
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
