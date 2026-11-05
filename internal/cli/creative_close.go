package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func creativeCloseCommand(open creativeConnection) *cobra.Command {
	return &cobra.Command{
		Use:   "close RUN WORKER_SESSION",
		Short: "Stop an own-run worker, preserving files, artifacts and history (orchestrator only)",
		Long:  "Explicitly stop a worker of the current Creative run orchestrator. Verify that completed assignment outputs are durably published before closing. Files, artifacts and history are preserved.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
				return &usageError{message: "usage: " + cmd.UseLine()}
			}
			return creativeArgs(1)(cmd, args[:1])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return creativeCall(cmd, open, "POST", apiPath("v1", "creative", "runs", args[0], "workers", args[1], "close"), nil)
		},
	}
}
