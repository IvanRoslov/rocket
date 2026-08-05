package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Управление постоянными агентами",
	}
	cmd.AddCommand(newAgentAddCmd())
	cmd.AddCommand(newAgentLsCmd())
	cmd.AddCommand(newAgentShowCmd())
	cmd.AddCommand(newAgentEditCmd())
	cmd.AddCommand(newAgentRmCmd())
	cmd.AddCommand(newAgentEnableCmd())
	cmd.AddCommand(newAgentDisableCmd())
	cmd.AddCommand(newAgentStartCmd())
	cmd.AddCommand(newAgentAttachCmd())
	cmd.AddCommand(newAgentStopCmd())
	cmd.AddCommand(newAgentAskCmd())
	cmd.AddCommand(newAgentQuestionsCmd())
	cmd.AddCommand(newAgentReplyCmd())
	cmd.AddCommand(newAgentCloseCmd(false))
	cmd.AddCommand(newAgentAnswerCmd())
	return cmd
}

func newAgentAddCmd() *cobra.Command {
	var description, project, dir, command string

	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Зарегистрировать агента (id = имя его tmux-сессии)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent add <id> [--description \"...\"] [--project <p>] [--dir <path>] [--command \"<cmd>\"]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"id": args[0]}
			for key, value := range map[string]string{
				"description": description, "project": project, "dir": dir, "command": command,
			} {
				if value != "" {
					reqBody[key] = value
				}
			}

			var resp map[string]any
			if err := c.Post("/v1/agents", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\t%s\n", toString(resp["id"]), dashIfEmpty(toString(resp["description"])))
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "описание для людей")
	cmd.Flags().StringVar(&project, "project", "", "проект для группировки в UI")
	cmd.Flags().StringVar(&dir, "dir", "", "рабочая директория агента (для rocket agent start)")
	cmd.Flags().StringVar(&command, "command", "", "команда запуска (пусто — интерактивный shell)")
	return cmd
}

func newAgentEditCmd() *cobra.Command {
	var description, project, dir, command string

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Изменить регистрацию агента",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent edit <id> [--description \"...\"] [--project <p>] [--dir <path>] [--command \"<cmd>\"]"}
			}

			reqBody := map[string]any{}
			for name, target := range map[string]*string{
				"description": &description, "project": &project, "dir": &dir, "command": &command,
			} {
				if cmd.Flags().Changed(name) {
					reqBody[name] = *target
				}
			}
			if len(reqBody) == 0 {
				return &usageError{message: "nothing to change: pass at least one of --description/--project/--dir/--command"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Patch(apiPath("v1", "agents", args[0]), reqBody, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\tupdated\n", toString(resp["id"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "описание для людей")
	cmd.Flags().StringVar(&project, "project", "", "проект для группировки в UI (пусто — вне проекта)")
	cmd.Flags().StringVar(&dir, "dir", "", "рабочая директория агента")
	cmd.Flags().StringVar(&command, "command", "", "команда запуска")
	return cmd
}

func newAgentLsCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Список агентов",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket agent ls [--project <p>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := "/v1/agents"
			if project != "" {
				path += "?project=" + project
			}
			var agents []map[string]any
			if err := c.Get(path, nil, &agents); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, agents)
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = tw.Write([]byte("ID\tPROJECT\tENABLED\tSESSION\tUNREAD\tQUESTIONS\tDESCRIPTION\n"))
			for _, a := range agents {
				_, _ = tw.Write([]byte(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					toString(a["id"]), dashIfEmpty(toString(a["project"])), toString(a["enabled"]),
					sessionCell(a["session_alive"]), dashIfZero(a["unread"]),
					questionsCell(a["open_questions"], a["awaiting_user"]),
					dashIfEmpty(toString(a["description"])))))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "показать только агентов этого проекта")
	return cmd
}

func newAgentShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Показать агента: регистрацию и инбокс",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent show <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var agent map[string]any
			if err := c.Get(apiPath("v1", "agents", args[0]), nil, &agent); err != nil {
				return err
			}
			var inbox []map[string]any
			if err := c.Get(apiPath("v1", "agents", args[0], "inbox")+"?status=unread", nil, &inbox); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, map[string]any{"agent": agent, "inbox": inbox})
			}

			cmd.Printf("id:          %s\n", toString(agent["id"]))
			cmd.Printf("description: %s\n", dashIfEmpty(toString(agent["description"])))
			cmd.Printf("project:     %s\n", dashIfEmpty(toString(agent["project"])))
			cmd.Printf("enabled:     %s\n", toString(agent["enabled"]))
			cmd.Printf("session:     %s\n", sessionCell(agent["session_alive"]))
			cmd.Printf("dir:         %s\n", dashIfEmpty(toString(agent["dir"])))
			cmd.Printf("command:     %s\n", dashIfEmpty(toString(agent["command"])))

			renderAgentMilestones(cmd.OutOrStdout(), agent["milestones"])

			cmd.Printf("unread: %d\n", len(inbox))
			for _, m := range inbox {
				cmd.Printf("  - #%s from %s: %s\n", toString(m["id"]),
					dashIfEmpty(toString(m["from"])), firstLine(toString(m["body"])))
			}
			return nil
		},
	}
}

func newAgentRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Удалить агента из реестра (его файлы остаются на диске)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent rm <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Delete(apiPath("v1", "agents", args[0]), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Println("deleted")
			return nil
		},
	}
}

func newAgentEnableCmd() *cobra.Command {
	return newAgentToggleCmd("enable", "Включить агента")
}

func newAgentDisableCmd() *cobra.Command {
	return newAgentToggleCmd("disable", "Выключить агента")
}

func newAgentToggleCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <id>",
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent " + action + " <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Post(apiPath("v1", "agents", args[0], action), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\tenabled=%s\n", toString(resp["id"]), toString(resp["enabled"]))
			return nil
		},
	}
}

// newAgentStartCmd runs the launcher: a tmux session named after the agent,
// running its own command in its own directory. Rocket manages nothing beyond
// creating it.
func newAgentStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <id>",
		Short: "Запустить tmux-сессию агента (dir + command из регистрации)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent start <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Post(apiPath("v1", "agents", args[0], "start"), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\trunning\t%s\n", toString(resp["id"]), toString(resp["dir"]))
			return nil
		},
	}
}

func newAgentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Short: "Убить tmux-сессию агента (регистрация остаётся)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent stop <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Post(apiPath("v1", "agents", args[0], "stop"), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\tstopped\n", toString(resp["id"]))
			return nil
		},
	}
}

// newAgentAttachCmd attaches to the agent's tmux session. An agent's session is
// named after it, so this needs no session lookup at all — which also means it
// works for a session a human created by hand before rocket noticed it.
func newAgentAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id>",
		Short: "Подключиться к tmux-сессии агента",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket agent attach <id>"}
			}

			argv := []string{"tmux", "attach", "-t", "=" + args[0]}
			// Inside an existing tmux client, attaching would nest one session
			// inside another; switch instead, keeping the exact-match target.
			if os.Getenv("TMUX") != "" {
				argv = []string{"tmux", "switch-client", "-t", "=" + args[0]}
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

// questionsCell renders an agent's open-thread counters as "open" or, when some
// of them wait on the human, "open (N ждут)".
func questionsCell(open, awaiting any) string {
	o := dashIfZero(open)
	if o == "-" {
		return "-"
	}
	if a := dashIfZero(awaiting); a != "-" {
		return o + " (" + a + " ждут)"
	}
	return o
}

// sessionCell renders the session_alive flag as a word, not a bool.
func sessionCell(alive any) string {
	if b, ok := alive.(bool); ok && b {
		return "live"
	}
	return "-"
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func dashIfZero(v any) string {
	s := toString(v)
	if s == "" || s == "0" {
		return "-"
	}
	return s
}
