package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// buildSendBody extracts the message body from args and/or filePath.
// Expects args to be [<session>, <text>?] where exactly one of the following holds:
//   - len(args) == 2 and filePath == "" (body from args[1])
//   - len(args) == 1 and filePath != "" (body from file)
//
// Returns an error if usage is violated or if body is empty.
func buildSendBody(args []string, filePath string) (string, error) {
	hasTextArg := len(args) == 2
	hasFileArg := filePath != ""

	// Check usage constraints
	if !hasTextArg && !hasFileArg {
		return "", fmt.Errorf("exactly one session id and one body source required")
	}
	if hasTextArg && hasFileArg {
		return "", fmt.Errorf("cannot use both positional body and --file")
	}

	var body string
	var err error

	if hasTextArg {
		body = args[1]
	} else {
		body, err = readFile(filePath)
		if err != nil {
			return "", err
		}
	}

	if body == "" {
		return "", fmt.Errorf("body must not be empty")
	}

	return body, nil
}

// readFile reads the entire contents of a file.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newSendCmd() *cobra.Command {
	var filePath string
	var wait bool

	cmd := &cobra.Command{
		Use:   "send <session> [<text>]",
		Short: "Отправить сообщение сессии",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate argument count
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: "usage: rocket send <session> [<text>] [--file <path>] [--wait]"}
			}

			// If --file is provided, args should be [session] only
			if filePath != "" && len(args) != 1 {
				return &usageError{message: "usage: rocket send <session> [<text>] [--file <path>] [--wait]"}
			}

			sessionID := args[0]

			// Build message body
			body, err := buildSendBody(args, filePath)
			if err != nil {
				return fmt.Errorf("invalid message: %v", err)
			}

			// Connect to daemon
			c, _, err := connect(true)
			if err != nil {
				return err
			}

			// Determine sender (from ROCKET_SESSION_ID env var)
			from := os.Getenv("ROCKET_SESSION_ID")

			// POST message
			reqBody := map[string]string{
				"to":   sessionID,
				"body": body,
			}
			if from != "" {
				reqBody["from"] = from
			}

			var resp struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			}
			if err := c.Post("/v1/messages", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}

			cmd.Printf("message %d queued\n", resp.ID)

			// If --wait, poll until delivered or failed
			if wait {
				return waitForMessage(cmd, c, resp.ID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to file containing message body")
	cmd.Flags().BoolVar(&wait, "wait", false, "poll until message is delivered or failed")

	return cmd
}

// waitForMessage polls GET /v1/messages/{id} every 2s until status is delivered or failed.
func waitForMessage(cmd *cobra.Command, c any, msgID int64) error {
	type msgResp struct {
		ID       int64  `json:"id"`
		Status   string `json:"status"`
		Attempts int    `json:"attempts"`
		Reason   string `json:"reason,omitempty"`
		Error    string `json:"error,omitempty"`
	}

	// Type assertion to get the actual client type
	client, ok := c.(interface {
		Get(path string, query map[string]string, out any) error
	})
	if !ok {
		return fmt.Errorf("internal error: invalid client type")
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			path := apiPath("v1", "messages", fmt.Sprintf("%d", msgID))
			var msg msgResp
			if err := client.Get(path, nil, &msg); err != nil {
				return err
			}

			switch msg.Status {
			case "delivered":
				cmd.Printf("message %d delivered\n", msgID)
				return nil
			case "failed":
				reason := msg.Reason
				if reason == "" && msg.Error != "" {
					reason = msg.Error
				}
				if reason == "" {
					reason = "unknown error"
				}
				cmd.PrintErrf("message %d failed: %s\n", msgID, reason)
				return fmt.Errorf("message delivery failed")
			case "queued":
				// Still waiting, continue polling
				continue
			default:
				return fmt.Errorf("unknown message status: %s", msg.Status)
			}
		}
	}
}
