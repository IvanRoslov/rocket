package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// sessionRow is the subset of the API's session JSON shape needed to render
// `rocket ls`. Field names/tags mirror internal/api.sessionResponse.
type sessionRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id"`
	RepoID    string `json:"repo_id"`
	State     string `json:"state"`
	Activity  string `json:"activity,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func newLsCmd() *cobra.Command {
	var project, feature string
	var all bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Список сессий",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket ls [--project <id>] [--feature <slug>] [--all]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			q := url.Values{}
			if project != "" {
				q.Set("project", project)
			}
			if feature != "" {
				q.Set("feature", feature)
			}
			if all {
				q.Set("all", "true")
			}
			path := "/v1/sessions"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}

			var sessions []sessionRow
			if err := c.Get(path, nil, &sessions); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, sessions)
			}

			renderSessions(sessions, cmd.OutOrStdout(), time.Now())
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "фильтр по id проекта")
	cmd.Flags().StringVar(&feature, "feature", "", "фильтр по slug фичи")
	cmd.Flags().BoolVar(&all, "all", false, "показать все сессии, включая завершённые")

	return cmd
}

// renderSessions writes the ls table to w: SESSION, KIND, PROJECT, REPO,
// STATE, ACTIVITY, AGE. PR/CI columns are omitted until phase 4. Empty
// activity renders as "-".
func renderSessions(sessions []sessionRow, w io.Writer, now time.Time) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = tw.Write([]byte("SESSION\tKIND\tPROJECT\tREPO\tSTATE\tACTIVITY\tAGE\n"))
	for _, s := range sessions {
		activity := s.Activity
		if activity == "" {
			activity = "-"
		}
		_, _ = tw.Write([]byte(fmt.Sprintf(
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.ID, s.Kind, s.ProjectID, s.RepoID, s.State, activity, humanAge(s.CreatedAt, now),
		)))
	}
	_ = tw.Flush()
}

// humanAge renders the time elapsed between unixSec and now as a short,
// human-friendly duration: "45s", "5m", "2h", "3d".
func humanAge(unixSec int64, now time.Time) string {
	d := now.Sub(time.Unix(unixSec, 0))
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// isTaskID reports whether s looks like a task id: a non-empty string of
// ASCII digits. Used by `rocket attach` to distinguish a task id (e.g.
// "12") from a session id (e.g. "demo-orch-a1b2").
func isTaskID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <session|task-id>",
		Short: "Подключиться к tmux-сессии агента (по id сессии или id задачи)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket attach <session|task-id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			sessionID := args[0]
			if isTaskID(args[0]) {
				var task taskDetailRow
				if err := c.Get(apiPath("v1", "tasks", args[0]), nil, &task); err != nil {
					return err
				}
				if task.Session == nil {
					return fmt.Errorf("task #%s has no session", args[0])
				}
				sessionID = task.Session.ID
			}

			var resp struct {
				Command []string `json:"command"`
			}
			if err := c.Get(apiPath("v1", "sessions", sessionID, "attach"), nil, &resp); err != nil {
				return err
			}
			if len(resp.Command) == 0 {
				return fmt.Errorf("daemon returned an empty attach command")
			}

			argv := resp.Command
			// Inside an existing tmux client, "tmux attach -t <target>" would
			// nest a tmux session inside another; switch-client instead,
			// keeping the same exact-match target.
			if os.Getenv("TMUX") != "" && len(argv) >= 2 && argv[0] == "tmux" && argv[1] == "attach" {
				argv = append([]string{"tmux", "switch-client"}, argv[2:]...)
			}

			if flags.JSON {
				return printJSON(cmd, map[string]any{"command": argv})
			}

			path, err := exec.LookPath(argv[0])
			if err != nil {
				return fmt.Errorf("%s: command not found: %w", argv[0], err)
			}
			return syscall.Exec(path, argv, os.Environ())
		},
	}
}

func newKillCmd() *cobra.Command {
	var cleanup, cascade bool

	cmd := &cobra.Command{
		Use:   "kill <session>",
		Short: "Остановить сессию",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket kill <session> [--cleanup] [--cascade]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			q := url.Values{}
			if cleanup {
				q.Set("cleanup", "true")
			}
			if cascade {
				q.Set("cascade", "true")
			}
			path := apiPath("v1", "sessions", args[0], "kill")
			if len(q) > 0 {
				path += "?" + q.Encode()
			}

			var resp map[string]any
			if err := c.Post(path, nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("killed %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&cleanup, "cleanup", false, "удалить worktree и tmux-сессию")
	cmd.Flags().BoolVar(&cascade, "cascade", false, "убить оркестратора вместе со всеми его воркерами")
	return cmd
}

func newRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <session>",
		Short: "Восстановить упавшую сессию",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket restore <session>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Post(apiPath("v1", "sessions", args[0], "restore"), nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("restored %s\n", args[0])
			return nil
		},
	}
}
