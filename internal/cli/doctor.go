package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/agent"
	_ "github.com/IvanRoslov/rocket/internal/agent/claudecode" // registers "claude-code" in agent.Registry()
	"github.com/spf13/cobra"
)

// checkStatus is the outcome of a single doctor check.
type checkStatus int

const (
	statusOK checkStatus = iota
	statusWarn
	statusFail
)

func (s checkStatus) icon() string {
	switch s {
	case statusOK:
		return "✅"
	case statusWarn:
		return "⚠️"
	default:
		return "❌"
	}
}

type checkResult struct {
	Status checkStatus
	Name   string
	Detail string
}

func (r checkResult) String() string {
	return fmt.Sprintf("%s %s: %s", r.Status.icon(), r.Name, r.Detail)
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Проверить окружение rocket",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket doctor"}
			}

			results := runDoctorChecks()

			w := cmd.OutOrStdout()
			anyFail := printDoctorResults(w, results)

			if anyFail {
				return &doctorFailedError{}
			}
			return nil
		},
	}
}

// doctorFailedError signals a doctor check failed (❌), mapping to exit
// code 1 like any other API/validation error.
type doctorFailedError struct{}

func (e *doctorFailedError) Error() string { return "one or more doctor checks failed" }

func printDoctorResults(w io.Writer, results []checkResult) (anyFail bool) {
	for _, r := range results {
		fmt.Fprintln(w, r.String())
		if r.Status == statusFail {
			anyFail = true
		}
	}
	return anyFail
}

// runDoctorChecks runs every doctor check and returns their results in a
// stable order: tmux, git, daemon, then each registered agent, then the
// Superpowers plugin check.
func runDoctorChecks() []checkResult {
	var results []checkResult

	results = append(results, checkTmux())
	results = append(results, checkGit())
	results = append(results, checkDaemon())
	results = append(results, checkAgents()...)
	results = append(results, checkSuperpowers())

	return results
}

func checkTmux() checkResult {
	path, err := exec.LookPath("tmux")
	if err != nil {
		return checkResult{statusFail, "tmux", "не найден в PATH"}
	}

	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return checkResult{statusWarn, "tmux", fmt.Sprintf("найден (%s), но `tmux -V` завершился с ошибкой", path)}
	}

	major, minor, ok := parseTmuxVersion(string(out))
	if !ok {
		return checkResult{statusWarn, "tmux", fmt.Sprintf("версия не распознана: %q", string(out))}
	}
	if major < 3 {
		return checkResult{statusFail, "tmux", fmt.Sprintf("версия %d.%d < 3.0", major, minor)}
	}
	return checkResult{statusOK, "tmux", fmt.Sprintf("%d.%d (%s)", major, minor, path)}
}

// tmuxVersionRE matches version strings like "tmux 3.6a" or "tmux 3.0".
var tmuxVersionRE = regexp.MustCompile(`(\d+)\.(\d+)`)

// parseTmuxVersion extracts the major/minor version from `tmux -V` output
// (e.g. "tmux 3.6a\n" -> 3, 6). ok is false if no version could be parsed.
func parseTmuxVersion(out string) (major, minor int, ok bool) {
	m := tmuxVersionRE.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func checkGit() checkResult {
	path, err := exec.LookPath("git")
	if err != nil {
		return checkResult{statusFail, "git", "не найден в PATH"}
	}
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return checkResult{statusWarn, "git", fmt.Sprintf("найден (%s), но `git --version` завершился с ошибкой", path)}
	}
	return checkResult{statusOK, "git", trimTrailingNewline(string(out))}
}

func checkDaemon() checkResult {
	c, _, err := connect(false)
	if err != nil {
		return checkResult{statusWarn, "daemon", "не запущен"}
	}

	var health struct {
		Version string `json:"version"`
		Uptime  string `json:"uptime"`
	}
	if err := c.Get("/v1/health", nil, &health); err != nil {
		return checkResult{statusWarn, "daemon", "не отвечает"}
	}
	return checkResult{statusOK, "daemon", fmt.Sprintf("запущен (version %s, uptime %s)", health.Version, health.Uptime)}
}

func checkAgents() []checkResult {
	var results []checkResult
	for name, a := range agent.Registry() {
		if err := a.Available(); err != nil {
			results = append(results, checkResult{statusFail, "agent:" + name, err.Error()})
			continue
		}
		results = append(results, checkResult{statusOK, "agent:" + name, "доступен"})
	}
	return results
}

// checkSuperpowers looks for the Superpowers Claude Code plugin, which
// rocket's orchestrator/worker prompts assume is installed. Its absence is
// a warning, not a failure — rocket itself still works.
func checkSuperpowers() checkResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return checkResult{statusWarn, "superpowers", "не удалось определить home directory"}
	}

	patterns := []string{
		filepath.Join(home, ".claude", "plugins", "cache", "*", "superpowers*"),
		filepath.Join(home, ".claude", "plugins", "superpowers"),
	}
	for _, pat := range patterns {
		matches, err := filepath.Glob(pat)
		if err == nil && len(matches) > 0 {
			return checkResult{statusOK, "superpowers", "плагин найден"}
		}
	}
	return checkResult{statusWarn, "superpowers", "плагин не найден — промпты rocket требуют плагин Superpowers"}
}

func trimTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
