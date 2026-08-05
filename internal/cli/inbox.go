package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Инбокс агента: непрочитанные сообщения (разбор — inbox next)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket inbox [--agent <id>]"}
			}

			id, err := resolveAgentID(agent)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var msgs []map[string]any
			if err := c.Get(apiPath("v1", "agents", id, "inbox")+"?status=unread", nil, &msgs); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, msgs)
			}

			cmd.Printf("%d unread\n", len(msgs))
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			for _, m := range msgs {
				_, _ = tw.Write([]byte(fmt.Sprintf("#%s\t%s\t%s\t%s\n",
					toString(m["id"]), dashIfEmpty(toString(m["from"])),
					age(m["created_at"]), firstLine(toString(m["body"])))))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "id агента (по умолчанию — агент текущей сессии)")

	cmd.AddCommand(newInboxNextCmd())
	cmd.AddCommand(newInboxPeekCmd())
	return cmd
}

// newInboxNextCmd takes the oldest unread message and marks it read. One at a
// time is the whole point: the agent processes a message, then asks for the
// next one, instead of being handed a wall of text it has to triage in one go.
func newInboxNextCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Взять самое старое непрочитанное сообщение и пометить прочитанным",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket inbox next [--agent <id>]"}
			}

			id, err := resolveAgentID(agent)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var msg map[string]any
			if err := c.Post(apiPath("v1", "agents", id, "inbox", "next"), nil, &msg); err != nil {
				return err
			}

			// A drained inbox answers 204: no body to decode.
			if len(msg) == 0 {
				if flags.JSON {
					return printJSON(cmd, map[string]any{"unread": 0})
				}
				cmd.Println("inbox is empty")
				return nil
			}

			if flags.JSON {
				return printJSON(cmd, msg)
			}
			printInboxMessage(cmd, msg)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "id агента (по умолчанию — агент текущей сессии)")
	return cmd
}

// newInboxPeekCmd reads one message in full without marking it read.
func newInboxPeekCmd() *cobra.Command {
	var agent string

	cmd := &cobra.Command{
		Use:   "peek <msg-id>",
		Short: "Прочитать сообщение целиком, не помечая прочитанным",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket inbox peek <msg-id> [--agent <id>]"}
			}
			if _, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64); err != nil {
				return &usageError{message: "message id must be a number, e.g. rocket inbox peek 12"}
			}

			id, err := resolveAgentID(agent)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var msg map[string]any
			if err := c.Get(apiPath("v1", "agents", id, "inbox", strings.TrimPrefix(args[0], "#")), nil, &msg); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, msg)
			}
			printInboxMessage(cmd, msg)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "id агента (по умолчанию — агент текущей сессии)")
	return cmd
}

// resolveAgentID decides which agent's inbox a command operates on. An agent's
// session is named after it, so the id is whatever the session is called: the
// explicit flag, else ROCKET_SESSION_ID (set by `rocket agent start`), else the
// name of the tmux session the command runs in (a session created by hand).
func resolveAgentID(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if id := os.Getenv("ROCKET_SESSION_ID"); id != "" {
		return id, nil
	}
	if id := tmuxSessionName(); id != "" {
		return id, nil
	}
	return "", &usageError{message: "agent is unknown: run this inside the agent's tmux session, " +
		"or pass --agent <id> (ROCKET_SESSION_ID is unset and no tmux session was detected)"}
}

// tmuxSessionName returns the name of the tmux session the process runs in, or
// "" when it runs outside tmux or tmux cannot answer.
func tmuxSessionName() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func printInboxMessage(cmd *cobra.Command, msg map[string]any) {
	cmd.Printf("#%s from %s (%s, %s)\n", toString(msg["id"]),
		dashIfEmpty(toString(msg["from"])), age(msg["created_at"]), toString(msg["status"]))
	cmd.Println()
	cmd.Println(toString(msg["body"]))
}

// firstLine returns the first line of body, shortened for a list view.
func firstLine(body string) string {
	line := body
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	// Counted in runes, not bytes: a Cyrillic question cut at byte 72 lands
	// mid-character and renders as garbage.
	const max = 72
	if r := []rune(line); len(r) > max {
		line = string(r[:max]) + "…"
	}
	return line
}

// age renders a JSON number of unix seconds as a compact age ("3m", "2h").
func age(v any) string {
	s := toString(v)
	if s == "" || s == "0" {
		return "-"
	}
	ts, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "-"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
