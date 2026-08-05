# CLI I/O Hygiene Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `rocket`'s CLI pipe-friendly: command results land on stdout, `--json` on the read commands carries everything the human output shows, and every text-writing command can take its body from a file or stdin instead of a shell-mangled argument.

**Architecture:** Three independent, mechanical changes inside `internal/cli`. (1) `NewRootCmd` calls `SetOut(os.Stdout)`, which flips cobra's `cmd.Print*` family from its `OutOrStderr()` default to stdout for every command at once. (2) A new `internal/cli/io.go` holds two shared helpers — `readTextInput` (file or `-` for stdin) and `textBody` (exactly-one-of positional-arg / `--file`) — that every writing command wires into its existing `RunE`. (3) `rocket task show --json` gains the docs/log/questions it already fetches for the human card.

**Tech Stack:** Go 1.x, `github.com/spf13/cobra` v1.3.0, stdlib `testing`.

## Global Constraints

- Diff stays inside `internal/cli` (plus its tests) and `docs/04-cli.md`. A parallel worker owns `internal/store` and `internal/api` — do not touch them.
- Do NOT touch the question thread model (participants, `--to`, attention sets). That is subtask core-attention's work.
- Exit codes are frozen (`docs/04-cli.md`): `0` ok, `1` API/validation, `2` daemon unavailable, `3` usage. Every new "both/neither body source" error must be a `*usageError` so it maps to 3.
- `connect()` panics-by-design under `go test` (`client_helpers.go:61`). Tests must never call a command's `RunE` past the point where it connects — test at flag-parsing / helper level, or assert on the usage error returned before `connect`.
- Existing `--json` shapes are consumed by web/mobile. Only ADD fields; never rename or remove one.
- Russian is the language of user-facing flag help, matching every other flag in the package.

---

### Task 1: Shared input helpers (`readTextInput`, `textBody`)

**Files:**
- Create: `internal/cli/io.go`
- Test: `internal/cli/io_test.go`

**Interfaces:**
- Consumes: `usageError` (`internal/cli/root.go:20`).
- Produces:
  - `const stdinMarker = "-"`
  - `func readTextInput(cmd *cobra.Command, path string) (string, error)`
  - `func textBody(cmd *cobra.Command, arg string, hasArg bool, filePath, usage string) (string, error)`

  `textBody` returns `&usageError{message: usage}` when both `hasArg` and a
  non-empty `filePath` are given, and when neither is. Tasks 3–5 call it as
  their single body-resolution point.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestReadTextInputFromFile reads a body off disk verbatim: markdown with
// backticks and newlines must survive, which is the whole point of --file.
func TestReadTextInputFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
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
	dir := t.TempDir()
	path := filepath.Join(dir, "body.md")
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
```

Add `"os"` and `"path/filepath"` to the import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run 'TestReadTextInput|TestTextBody' -v`
Expected: FAIL — `undefined: readTextInput`, `undefined: stdinMarker`, `undefined: textBody`.

- [ ] **Step 3: Write minimal implementation**

```go
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
// substitution and its $words expanded, so agents lose exactly the content
// they were trying to send. A pipe (or a file) never touches the shell's
// parser.
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run 'TestReadTextInput|TestTextBody' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/io.go internal/cli/io_test.go
git commit -m "cli: общие хелперы ввода текста — --file и '-' (stdin)"
```

---

### Task 2: Route command results to stdout

**Files:**
- Modify: `internal/cli/root.go:28-58` (`NewRootCmd`)
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: no new symbols. After this task every `cmd.Print*` call under
  the root command writes to stdout instead of stderr.

**Background (verify before you start, do not take on faith):** cobra v1.3.0
defines `func (c *Command) Print(i ...interface{}) { fmt.Fprint(c.OutOrStderr(), i...) }`
(`$(go env GOMODCACHE)/github.com/spf13/cobra@v1.3.0/command.go:1207`).
`OutOrStderr()` returns `c.outWriter` when set and `os.Stderr` otherwise, so
today every `cmd.Printf("task #%d created\n", ...)` in this package goes to
stderr. Confirmed at runtime: `rocket send <s> hi 1>/dev/null` still prints
`message N queued`. Setting `outWriter` once on the root fixes all of them,
because subcommands inherit it through `getOut`'s parent walk.
`PrintErr*` uses `ErrOrStderr()`/`errWriter` and is unaffected — diagnostics
stay on stderr, which is what `send.go:196` and `send.go:204` want.

- [ ] **Step 1: Write the failing test**

```go
// TestRootRoutesResultsToStdout pins the pipe contract: a command's result
// goes to stdout, so `rocket ... | grep` and `... | jq` work. cobra's
// Print/Printf/Println default to OutOrStderr(), so without an explicit
// SetOut on the root every result line would land on stderr — the bug this
// guards against.
func TestRootRoutesResultsToStdout(t *testing.T) {
	root := NewRootCmd()

	var out, errBuf bytes.Buffer
	// Deliberately NOT calling root.SetOut: the point is that NewRootCmd
	// already installed a stdout writer. Reach for it through the API
	// cobra's Print* uses.
	if got := root.OutOrStderr(); got != os.Stdout {
		t.Fatalf("root.OutOrStderr() = %v, want os.Stdout", got)
	}

	// And once a test overrides it, Print* must honour the override rather
	// than the hardcoded os.Stdout — otherwise no command stays testable.
	root.SetOut(&out)
	root.SetErr(&errBuf)
	sub := &cobra.Command{Use: "probe", RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("result line")
		cmd.PrintErrln("diagnostic line")
		return nil
	}}
	root.AddCommand(sub)
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
```

Ensure `bytes`, `os`, `strings`, `testing` and `github.com/spf13/cobra` are imported.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run TestRootRoutesResultsToStdout -v`
Expected: FAIL — `root.OutOrStderr() = &{...os.Stderr}, want os.Stdout`.

- [ ] **Step 3: Write minimal implementation**

In `NewRootCmd`, immediately after the `root := &cobra.Command{...}` literal:

```go
	// cobra's Print/Printf/Println write to OutOrStderr(), i.e. stderr
	// unless an out writer is set. Every command in this package uses them
	// to print its RESULT, and a result on stderr cannot be piped into
	// grep or jq. Setting the writer once on the root fixes all of them:
	// subcommands inherit it. Diagnostics keep using PrintErr*, which
	// reads errWriter and is unaffected.
	root.SetOut(os.Stdout)
```

`os` is already imported by `root.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run TestRootRoutesResultsToStdout -v`
Expected: PASS.

Then check nothing relied on results-in-stderr:

Run: `go test ./... 2>&1 | tail -30`
Expected: all packages ok. If a test asserts a result string on a stderr
buffer, fix the test to read the stdout buffer — the production behaviour is
the thing being corrected.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "cli: результаты команд идут в stdout, а не в stderr"
```

---

### Task 3: `--file` on the task text-writing commands

**Files:**
- Modify: `internal/cli/task.go:599-646` (`newTaskLogCmd`), `:652-698` (`newTaskAskCmd`), `:706-747` (`newTaskAskOrchCmd`), `:846-885` (`newTaskReplyCmd`), `:887-945` (`newTaskAnswerCmd`)
- Test: `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `textBody`, `stdinMarker` (Task 1).
- Produces: no new exported symbols; five commands gain a `--file` flag.

Every one of the five follows the same shape. Taking `newTaskLogCmd` as the
worked example, the changes are:

1. Declare `var file string` alongside `var kind string`.
2. Register `cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")`.
3. Relax the arg-count check: the text argument is now optional.
4. Resolve the body through `textBody` before `connect`.

- [ ] **Step 1: Write the failing test**

```go
// TestTaskWritingCommandsFileFlag pins that every text-writing task command
// accepts --file (and '-' for stdin) as an alternative to the positional
// text, and that supplying both or neither is a usage error (exit 3).
//
// These cases all resolve the body BEFORE connect(), which is disabled
// under go test, so only the usage-error paths run to completion here; the
// happy path is covered at the textBody level in io_test.go.
func TestTaskWritingCommandsFileFlag(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body.md")
	if err := os.WriteFile(body, []byte("markdown `body`"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"log both", newTaskLogCmd, []string{"1", "text", "--kind", "note", "--file", body}},
		{"log neither", newTaskLogCmd, []string{"1", "--kind", "note"}},
		{"ask both", newTaskAskCmd, []string{"1", "text", "--file", body}},
		{"ask neither", newTaskAskCmd, []string{"1"}},
		{"ask-orch both", newTaskAskOrchCmd, []string{"1", "text", "--file", body}},
		{"ask-orch neither", newTaskAskOrchCmd, []string{"1"}},
		{"reply both", newTaskReplyCmd, []string{"7", "text", "--file", body}},
		{"reply neither", newTaskReplyCmd, []string{"7"}},
		{"answer both", newTaskAnswerCmd, []string{"7", "text", "--file", body}},
		{"answer file plus dismiss", newTaskAnswerCmd, []string{"7", "--dismiss", "--file", body}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.new()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected a usage error, got nil")
			}
			if code := exitCode(err); code != 3 {
				t.Fatalf("exitCode = %d, want 3 (err=%v)", code, err)
			}
		})
	}
}

// TestTaskLogFileFlagRegistered proves the flag exists and is wired to the
// stdin marker, independent of the daemon.
func TestTaskLogFileFlagRegistered(t *testing.T) {
	for name, newCmd := range map[string]func() *cobra.Command{
		"log":       newTaskLogCmd,
		"ask":       newTaskAskCmd,
		"ask-orch":  newTaskAskOrchCmd,
		"reply":     newTaskReplyCmd,
		"answer":    newTaskAnswerCmd,
	} {
		if f := newCmd().Flags().Lookup("file"); f == nil {
			t.Errorf("rocket task %s: --file flag is not registered", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run 'TestTaskWritingCommandsFileFlag|TestTaskLogFileFlagRegistered' -v`
Expected: FAIL — `--file flag is not registered`, and the "both" cases fail
with cobra's `unknown flag: --file`.

- [ ] **Step 3: Write minimal implementation**

`newTaskLogCmd` — replace the head of `RunE` and the flag block:

```go
func newTaskLogCmd() *cobra.Command {
	var kind string
	var file string

	const usage = "usage: rocket task log <id> --kind <k> \"<text>\" | --file <path>"

	cmd := &cobra.Command{
		Use:   "log <id> \"<text>\"",
		Short: "Добавить запись в журнал задачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			if kind == "" {
				return &usageError{message: usage}
			}

			taskID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return &usageError{message: "invalid task id"}
			}

			text, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			// ... unchanged from here: connect, POST, print
```

and

```go
	cmd.Flags().StringVar(&kind, "kind", "", "тип записи (decision|problem|note|status)")
	cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")
```

Add this tiny helper to `internal/cli/io.go` (it keeps the five call sites
from each repeating a bounds check):

```go
// argAt returns args[i], or "" when the slice is shorter. Callers pair it
// with their own len(args) check to tell "absent" from "empty".
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
```

`newTaskAskCmd` — same treatment:

```go
	var context string
	var to []string
	var file string

	const usage = "usage: rocket task ask <task-id> \"<вопрос>\" | --file <path> [--context <md>] [--to <id,...>]"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}

			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}

			if os.Getenv("ROCKET_SESSION_ID") == "" {
				return errors.New("rocket task ask is for orchestrators asking the human; to ask the orchestrator a question use: rocket task ask-orch")
			}
			...
			reqBody := map[string]any{"body": body}
```

and `cmd.Flags().StringVar(&file, "file", "", "файл с вопросом ('-' — stdin)")`.

`newTaskAskOrchCmd` — identical to `newTaskAskCmd` (its usage string names
`ask-orch`, and it has no `ROCKET_SESSION_ID` check).

`newTaskReplyCmd`:

```go
	var to []string
	var taskFlag int64
	var file string

	const usage = "usage: rocket task reply <question-id>|<task-id>/Q<n> \"<текст>\" | --file <path> [--task <task-id>] [--to <id,...>]"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			id, err := resolveQuestionRef(args[0], taskFlag)
			...
			reqBody := map[string]any{"body": body}
```

and `cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")`.

`newTaskAnswerCmd` — three mutually exclusive sources now (positional,
`--file`, `--dismiss`), so `textBody` is only consulted when not dismissing:

```go
	var dismiss bool
	var to []string
	var taskFlag int64
	var file string

	const usage = "usage: rocket task answer <question-id>|<task-id>/Q<n> \"<ответ>\" | --file <path> | --dismiss (exactly one)"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			hasArg := len(args) == 2
			// --dismiss closes the thread with no answer at all, so it
			// excludes BOTH body sources rather than just the positional
			// one.
			if dismiss && (hasArg || file != "") {
				return &usageError{message: usage}
			}

			var body string
			if !dismiss {
				var err error
				body, err = textBody(cmd, argAt(args, 1), hasArg, file, usage)
				if err != nil {
					return err
				}
			}
			...
			reqBody := map[string]any{}
			if dismiss {
				reqBody["dismiss"] = true
			} else {
				reqBody["body"] = body
			}
```

and `cmd.Flags().StringVar(&file, "file", "", "файл с ответом ('-' — stdin)")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run 'TestTaskWritingCommandsFileFlag|TestTaskLogFileFlagRegistered' -v`
Expected: PASS.

Run: `go test ./internal/cli`
Expected: ok — in particular the existing usage-error tests for these
commands must still expect exit 3.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/io.go internal/cli/task.go internal/cli/task_test.go
git commit -m "cli: --file/stdin для task log|ask|ask-orch|reply|answer"
```

---

### Task 4: `--file` on the agent text-writing commands

**Files:**
- Modify: `internal/cli/agent_questions.go:38-76` (`newAgentAskCmd`), `:127-165` (`newAgentReplyCmd`), `:169-224` (`newAgentAnswerCmd`)
- Test: `internal/cli/agent_questions_test.go`

**Interfaces:**
- Consumes: `textBody`, `argAt` (Task 1 + Task 3).
- Produces: three commands gain a `--file` flag, same semantics as Task 3.

- [ ] **Step 1: Write the failing test**

```go
// TestAgentWritingCommandsFileFlag mirrors the task-side test: --file is an
// alternative to the positional text on every role-thread writing command,
// and both/neither is a usage error (exit 3).
func TestAgentWritingCommandsFileFlag(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body.md")
	if err := os.WriteFile(body, []byte("markdown `body`"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"ask both", newAgentAskCmd, []string{"sre", "text", "--file", body}},
		{"ask neither", newAgentAskCmd, []string{"sre"}},
		{"reply both", newAgentReplyCmd, []string{"sre/Q1", "text", "--file", body}},
		{"reply neither", newAgentReplyCmd, []string{"sre/Q1"}},
		{"answer both", newAgentAnswerCmd, []string{"sre/Q1", "text", "--file", body}},
		{"answer file plus dismiss", newAgentAnswerCmd, []string{"sre/Q1", "--dismiss", "--file", body}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.new()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected a usage error, got nil")
			}
			if code := exitCode(err); code != 3 {
				t.Fatalf("exitCode = %d, want 3 (err=%v)", code, err)
			}
		})
	}
}

// TestAgentWritingCommandsRegisterFileFlag proves the flag exists on each.
func TestAgentWritingCommandsRegisterFileFlag(t *testing.T) {
	for name, newCmd := range map[string]func() *cobra.Command{
		"ask":    newAgentAskCmd,
		"reply":  newAgentReplyCmd,
		"answer": newAgentAnswerCmd,
	} {
		if f := newCmd().Flags().Lookup("file"); f == nil {
			t.Errorf("rocket agent %s: --file flag is not registered", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run 'TestAgentWritingCommands' -v`
Expected: FAIL — `--file flag is not registered` / `unknown flag: --file`.

- [ ] **Step 3: Write minimal implementation**

`newAgentAskCmd`:

```go
	var context string
	var to []string
	var file string

	const usage = "usage: rocket agent ask <role> \"<вопрос>\" | --file <path> [--context <md>] [--to <id,...>]"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			...
			reqBody := map[string]any{"body": body}
```

plus `cmd.Flags().StringVar(&file, "file", "", "файл с вопросом ('-' — stdin)")`.

`newAgentReplyCmd`:

```go
	var to []string
	var file string

	const usage = "usage: rocket agent reply <question-id>|<role>/Q<n> \"<текст>\" | --file <path> [--to <id,...>]"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			id, err := resolveAgentQuestionRef(args[0])
			...
			reqBody := map[string]any{"body": body}
```

plus `cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")`.

`newAgentAnswerCmd` — same three-way exclusion as `newTaskAnswerCmd`:

```go
	var dismiss bool
	var to []string
	var file string

	const usage = "usage: rocket agent answer <question-id>|<role>/Q<n> \"<ответ>\" | --file <path> | --dismiss (exactly one)"
	...
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			hasArg := len(args) == 2
			if dismiss && (hasArg || file != "") {
				return &usageError{message: usage}
			}

			var body string
			if !dismiss {
				var err error
				body, err = textBody(cmd, argAt(args, 1), hasArg, file, usage)
				if err != nil {
					return err
				}
			}
			...
			if dismiss {
				reqBody["dismiss"] = true
			} else {
				reqBody["body"] = body
			}
```

plus `cmd.Flags().StringVar(&file, "file", "", "файл с ответом ('-' — stdin)")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run 'TestAgentWritingCommands' -v`
Expected: PASS.

Run: `go test ./internal/cli`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/agent_questions.go internal/cli/agent_questions_test.go
git commit -m "cli: --file/stdin для agent ask|reply|answer"
```

---

### Task 5: `rocket send --file -` reads stdin

**Files:**
- Modify: `internal/cli/send.go:24-50` (`buildSendBody`, `readFile`)
- Test: `internal/cli/send_test.go`

**Interfaces:**
- Consumes: `readTextInput` (Task 1).
- Produces: `buildSendBody` gains a `*cobra.Command` first parameter —
  `func buildSendBody(cmd *cobra.Command, args []string, filePath string) (string, error)`
  — because stdin now has to come from the command, not the process. The
  package-level `readFile` helper is deleted; `readTextInput` replaces it.

`send` already documents `--file`, so this task only adds the `-` case and
brings it onto the shared helper. Its own XOR validation in `RunE` stays as
it is (it predates `textBody` and its usage string is asserted by existing
tests).

- [ ] **Step 1: Write the failing test**

```go
// TestBuildSendBodyFromStdin covers `rocket send <session> --file -`: the
// body is piped in, so markdown reaches the recipient with its backticks
// intact instead of being run by the shell.
func TestBuildSendBodyFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("piped `body`"))

	got, err := buildSendBody(cmd, []string{"session-id"}, stdinMarker)
	if err != nil {
		t.Fatalf("buildSendBody: %v", err)
	}
	if got != "piped `body`" {
		t.Errorf("got %q, want %q", got, "piped `body`")
	}
}

// TestBuildSendBodyEmptyStdin keeps the non-empty guarantee: an empty pipe
// is a rejected body, not a silently-sent blank message.
func TestBuildSendBodyEmptyStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))

	if _, err := buildSendBody(cmd, []string{"session-id"}, stdinMarker); err == nil {
		t.Fatal("expected an error for an empty piped body, got nil")
	}
}
```

Update the two existing tests (`TestBuildSendBodyUsageViolations`,
`TestBuildSendBodyContent`) to pass `&cobra.Command{}` as the new first
argument.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run TestBuildSendBody -v`
Expected: FAIL — `too many arguments in call to buildSendBody` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `send.go`, change the signature and drop the local `readFile`:

```go
func buildSendBody(cmd *cobra.Command, args []string, filePath string) (string, error) {
	var body string
	var err error

	if filePath != "" {
		body, err = readTextInput(cmd, filePath)
		if err != nil {
			return "", err
		}
	} else if len(args) == 2 {
		body = args[1]
	}

	if body == "" {
		return "", fmt.Errorf("body must not be empty")
	}

	return body, nil
}
```

Delete the `readFile` function (nothing else calls it — confirm with
`grep -rn 'readFile(' internal/`). Update the call site in `RunE` to
`buildSendBody(cmd, args, filePath)`, and the `--file` flag help to
`"файл с текстом сообщения ('-' — stdin)"`. Remove the now-unused `"os"`
import only if nothing else in the file uses it (`os.Getenv` does — keep it).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run TestBuildSendBody -v`
Expected: PASS.

Run: `go build ./... && go test ./internal/cli`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/send.go internal/cli/send_test.go
git commit -m "cli: rocket send --file - читает stdin"
```

---

### Task 6: `rocket task show --json` carries docs, log and questions

**Files:**
- Modify: `internal/cli/task.go:372-424` (`newTaskShowCmd`)
- Test: `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `taskDetailRow`, `taskDocRow`, `taskLogRow`, `questionRow`,
  `fetchQuestions` (all already in `task.go`).
- Produces: `type taskShowJSON struct` — the task fields inlined, plus
  `docs`, `log` and `questions`.

**Why:** the human card (`renderTaskCard`) shows subtasks, docs, the journal
and the question threads. `--json` short-circuits before fetching three of
those four and emits only the task row, so `rocket task show 5 --json` is
strictly less informative than `rocket task show 5`. That is the gap the
brief's "audit that all fields shown in human output are present" names.

Audit result for the other three commands named in the brief — no change
needed, and Step 1 pins each so it stays that way:
- `task ls --json` emits `{"board": {...}}` of `taskRow`, the exact rows
  `renderTaskBoard` draws from.
- `task questions --json` emits `questionRow`, which is a superset of what
  `renderQuestions` prints (it also carries `context`, `participants`,
  `waiting_on`, `messages`).
- `agent questions --json` emits `agentQuestionRow`, likewise a superset of
  `renderAgentQuestions`.

- [ ] **Step 1: Write the failing test**

```go
// TestTaskShowJSONCarriesEverythingTheCardShows pins that --json is not
// poorer than the human card: whatever renderTaskCard draws (subtasks,
// docs, journal, question threads) must have a home in the JSON shape.
// Marshalling the struct and checking the keys is enough — the values come
// from the same three API calls the card makes.
func TestTaskShowJSONCarriesEverythingTheCardShows(t *testing.T) {
	v := taskShowJSON{
		taskDetailRow: taskDetailRow{ID: 5, Title: "t", Subtasks: []taskRow{{ID: 6}}},
		Docs:          []taskDocRow{{ID: 1, Kind: "spec"}},
		Log:           []taskLogRow{{ID: 2, Kind: "decision"}},
		Questions:     []questionRow{{ID: 3, Ordinal: 1}},
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The task's own fields must stay at the top level: web and mobile
	// already read `id`/`title`/`subtasks` from there, and --json may only
	// gain keys, never move them.
	for _, key := range []string{"id", "title", "subtasks", "docs", "log", "questions"} {
		if _, ok := got[key]; !ok {
			t.Errorf("task show --json is missing key %q; got keys %v", key, got)
		}
	}
}
```

Ensure `encoding/json` is imported by the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli -run TestTaskShowJSONCarriesEverything -v`
Expected: FAIL — `undefined: taskShowJSON`.

- [ ] **Step 3: Write minimal implementation**

Add above `newTaskShowCmd`:

```go
// taskShowJSON is what `rocket task show --json` emits: the task's own
// fields inlined at the top level (web and mobile already read id/title/
// subtasks from there, so they must not move) plus the three collections
// the human card also shows. Before this, --json returned the bare task row
// and was strictly poorer than the text output it is supposed to mirror.
type taskShowJSON struct {
	taskDetailRow
	Docs      []taskDocRow  `json:"docs"`
	Log       []taskLogRow  `json:"log"`
	Questions []questionRow `json:"questions"`
}
```

Then in `RunE`, delete the early `if flags.JSON { return printJSON(cmd, task) }`
block, and after the docs/log/questions fetches replace the render call with:

```go
			if flags.JSON {
				return printJSON(cmd, taskShowJSON{
					taskDetailRow: task,
					Docs:          docsResp.Docs,
					Log:           logResp.Log,
					Questions:     questions,
				})
			}

			renderTaskCard(task, docsResp.Docs, logResp.Log, questions, cmd.OutOrStdout(), time.Now())
			return nil
```

Note `docs`/`log`/`questions` have no `omitempty`: a machine consumer wants
an empty array, not a missing key. `printJSON` renders a nil slice as
`null`; initialise each to an empty slice when nil so the shape is stable:

```go
			docs := docsResp.Docs
			if docs == nil {
				docs = []taskDocRow{}
			}
			logEntries := logResp.Log
			if logEntries == nil {
				logEntries = []taskLogRow{}
			}
			if questions == nil {
				questions = []questionRow{}
			}
```

and use those three in the struct literal (and pass `docs`, `logEntries`,
`questions` to `renderTaskCard` — it already handles empty slices).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli -run TestTaskShowJSONCarriesEverything -v`
Expected: PASS.

Run: `go test ./internal/cli`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/task.go internal/cli/task_test.go
git commit -m "cli: task show --json отдаёт доки, журнал и вопросы"
```

---

### Task 7: Document the new flags

**Files:**
- Modify: `docs/04-cli.md:3`, `:16-22`, `:72`, `:122-133`, `:163-176`

**Interfaces:**
- Consumes: the flags added in Tasks 3–5.
- Produces: nothing in code.

`docs/04-cli.md` is the CLI's spec — it lists every flag of every command.
Leaving it unchanged would make the spec disagree with the binary. It is
outside `internal/cli` but carries no conflict risk with the parallel
`internal/store`/`internal/api` worker.

- [ ] **Step 1: Update the global-flags paragraph (line 3)**

Append to the sentence about global flags:

```
Результат команды печатается в stdout (можно пайпить в grep/jq), диагностика — в stderr.
Везде, где команда принимает текст позиционным аргументом, есть и `--file <path>`
(`-` — читать stdin); указывать оба сразу — ошибка использования (код 3).
```

- [ ] **Step 2: Update the per-command lines**

```
rocket task log <id> --kind decision|problem|note "<текст>" | --file <f>
rocket task ask-orch <id> "<вопрос>" | --file <f> [--context <md>] [--to a,b]
rocket task reply <вопрос> "<уточнение>" | --file <f> [--to a,b]
rocket task answer <вопрос> "<ответ>" | --file <f> [--to a,b]
```

```
rocket send <session> "<текст>" | --file <path>    # --file - читает stdin
```

```
rocket agent ask <id> "<вопрос>" | --file <f> [--context <md>] [--to a,b]
rocket agent reply <вопрос> "<текст>" | --file <f> [--to a,b]
rocket agent answer <вопрос> "<ответ>" | --file <f> | --dismiss
```

```
rocket task ask <id> "<вопрос>" | --file <f> [--context <md>] [--to a,b]
```

- [ ] **Step 3: Note the richer `task show --json`**

Under the `rocket task show <id>` line add:

```
    --json отдаёт ту же карточку целиком: поля задачи + docs, log, questions.
```

- [ ] **Step 4: Commit**

```bash
git add docs/04-cli.md
git commit -m "docs: --file/stdin, stdout-контракт и task show --json в спеке CLI"
```

---

## Final verification (before the PR)

- [ ] `gofmt -l internal/cli docs` prints nothing.
- [ ] `go vet ./...` is clean.
- [ ] `go test ./...` passes.
- [ ] `go build -o /tmp/rocket-io ./cmd/rocket` succeeds, then exercise the
      acceptance criteria against the live daemon from the worktree:
      - `/tmp/rocket-io task show 1025 2>/dev/null | head` still prints the card
        (proves results are on stdout).
      - `/tmp/rocket-io task show 1025 --json | jq -e '.docs, .log, .questions'`
        succeeds.
      - `/tmp/rocket-io task questions 1025 --json | jq .` succeeds.
      - `printf 'note with a \x60backtick\x60\n' | /tmp/rocket-io task log 1025 --kind note --file -`
        succeeds, and `/tmp/rocket-io task show 1025` shows the backtick intact.
      - `/tmp/rocket-io task log 1025 --kind note "x" --file -` exits 3.
- [ ] `rocket task log 1025 --kind decision "..."` for each decision worth keeping.
