package cli

import (
	"errors"
	"os"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/version"
	"github.com/spf13/cobra"
)

type globalFlags struct {
	JSON   bool
	Socket string
}

var flags globalFlags

// Error types for exit code mapping
type usageError struct {
	message string
}

func (e *usageError) Error() string {
	return e.message
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "rocket",
		Short:         "Оркестрация AI-кодинг-агентов поверх tmux и git worktree",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&flags.JSON, "json", false, "машинный вывод")
	root.PersistentFlags().StringVar(&flags.Socket, "socket", "", "путь к сокету демона")
	root.AddCommand(newDaemonCmd())
	root.AddCommand(newRepoCmd())
	return root
}

// Execute returns the exit code for the process (0/1/2/3).
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		os.Stderr.WriteString("rocket: " + err.Error() + "\n")
		return exitCode(err)
	}
	return 0
}

// exitCode maps error types to exit codes:
// 0: success
// 1: API/validation error (default)
// 2: daemon unavailable
// 3: usage error
func exitCode(err error) int {
	var usageErr *usageError

	if errors.As(err, &usageErr) {
		return 3
	}
	if errors.Is(err, client.ErrDaemonUnavailable) {
		return 2
	}
	return 1
}
