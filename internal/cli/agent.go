package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Управление ролями (постоянными агентами)",
	}
	cmd.AddCommand(newAgentAddCmd())
	cmd.AddCommand(newAgentLsCmd())
	cmd.AddCommand(newAgentShowCmd())
	cmd.AddCommand(newAgentRmCmd())
	cmd.AddCommand(newAgentEnableCmd())
	cmd.AddCommand(newAgentDisableCmd())
	cmd.AddCommand(newAgentWakeCmd())
	cmd.AddCommand(newAgentStateCmd())
	cmd.AddCommand(newAgentAskCmd())
	cmd.AddCommand(newAgentQuestionsCmd())
	cmd.AddCommand(newAgentReplyCmd())
	cmd.AddCommand(newAgentAnswerCmd())
	return cmd
}

// parseWatch parses a --watch value: "owner/repo[,label=bug][,mention-only]".
func parseWatch(spec string) (map[string]any, error) {
	parts := strings.Split(spec, ",")
	repo := strings.TrimSpace(parts[0])
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, fmt.Errorf("invalid --watch %q: expected owner/repo[,label=...][,mention-only]", spec)
	}

	sub := map[string]any{"repo": repo}
	var labels []string
	for _, opt := range parts[1:] {
		opt = strings.TrimSpace(opt)
		switch {
		case opt == "":
		case opt == "mention-only":
			sub["mention_only"] = true
		case strings.HasPrefix(opt, "label="):
			label := strings.TrimSpace(strings.TrimPrefix(opt, "label="))
			if label == "" {
				return nil, fmt.Errorf("invalid --watch %q: empty label", spec)
			}
			labels = append(labels, label)
		default:
			return nil, fmt.Errorf("invalid --watch %q: unknown option %q", spec, opt)
		}
	}
	if len(labels) > 0 {
		sub["labels"] = labels
	}
	return sub, nil
}

// roleFromSessionID maps a role instance session id ("sre-run-3") to its role
// ("sre"). Returns "" for any other session id.
func roleFromSessionID(sessionID string) string {
	i := strings.LastIndex(sessionID, "-run-")
	if i <= 0 {
		return ""
	}
	return sessionID[:i]
}

// parseUntil accepts "YYYY-MM-DD" or RFC3339 and returns unix seconds.
func parseUntil(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Unix(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0, fmt.Errorf("invalid date %q: use YYYY-MM-DD or RFC3339", s)
	}
	return t.Unix(), nil
}

// splitItemRef splits "<kind>:<ref>" into its parts, e.g.
// "issue:acme/platform#12" or "task:45".
func splitItemRef(s string) (kind, ref string, err error) {
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", fmt.Errorf("invalid item %q: expected <kind>:<ref>, e.g. issue:owner/repo#12", s)
	}
	return s[:i], s[i+1:], nil
}

func newAgentAddCmd() *cobra.Command {
	var project, promptFile, cron, agent string
	var watch []string

	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Зарегистрировать роль",
		RunE: func(cmd *cobra.Command, args []string) error {
			const usage = "usage: rocket agent add <id> --project <p> --prompt-file <f> [--watch owner/repo[,label=...][,mention-only]]... [--cron expr] [--agent name]"
			if len(args) != 1 || project == "" || promptFile == "" {
				return &usageError{message: usage}
			}

			prompt, err := os.ReadFile(promptFile)
			if err != nil {
				return fmt.Errorf("read prompt file: %w", err)
			}

			subs := make([]map[string]any, 0, len(watch))
			for _, spec := range watch {
				sub, err := parseWatch(spec)
				if err != nil {
					return &usageError{message: err.Error()}
				}
				subs = append(subs, sub)
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"id":            args[0],
				"project":       project,
				"prompt":        string(prompt),
				"subscriptions": subs,
			}
			if cron != "" {
				reqBody["cron"] = cron
			}
			if agent != "" {
				reqBody["agent"] = agent
			}

			var resp map[string]any
			if err := c.Post("/v1/agents", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s\t%s\t%s\n", toString(resp["id"]), toString(resp["project"]), toString(resp["prompt_path"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "id проекта роли (обязательно)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "файл роль-промпта (обязательно)")
	cmd.Flags().StringArrayVar(&watch, "watch", nil, "подписка owner/repo[,label=...][,mention-only] (можно несколько раз)")
	cmd.Flags().StringVar(&cron, "cron", "", "расписание регулярных пробуждений")
	cmd.Flags().StringVar(&agent, "agent", "", "underlying-агент (claude-code|codex)")
	return cmd
}

func newAgentLsCmd() *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Список ролей",
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
			_, _ = tw.Write([]byte("ID\tPROJECT\tENABLED\tINBOX\tITEMS\tQUESTIONS\tCRON\n"))
			for _, a := range agents {
				_, _ = tw.Write([]byte(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					toString(a["id"]), toString(a["project"]), toString(a["enabled"]),
					toString(a["inbox_queued"]), toString(a["items"]),
					questionsCell(a["open_questions"], a["awaiting_user"]),
					dashIfEmpty(toString(a["cron"])))))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "показать только роли этого проекта")
	return cmd
}

func newAgentShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Показать роль: определение, инбокс и досье",
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
			if err := c.Get(apiPath("v1", "agents", args[0], "inbox")+"?status=queued", nil, &inbox); err != nil {
				return err
			}
			var items []map[string]any
			if err := c.Get(apiPath("v1", "agents", args[0], "items"), nil, &items); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, map[string]any{"agent": agent, "inbox": inbox, "items": items})
			}

			cmd.Printf("id:      %s\n", toString(agent["id"]))
			cmd.Printf("project: %s\n", toString(agent["project"]))
			cmd.Printf("enabled: %s\n", toString(agent["enabled"]))
			cmd.Printf("agent:   %s\n", toString(agent["agent"]))
			cmd.Printf("cron:    %s\n", dashIfEmpty(toString(agent["cron"])))
			cmd.Printf("prompt:  %s\n", toString(agent["prompt_path"]))

			cmd.Println("subscriptions:")
			if subs, ok := agent["subscriptions"].([]any); ok && len(subs) > 0 {
				for _, s := range subs {
					if sub, ok := s.(map[string]any); ok {
						cmd.Printf("  - %s%s%s\n", toString(sub["repo"]),
							labelsSuffix(sub["labels"]), mentionSuffix(sub["mention_only"]))
					}
				}
			} else {
				cmd.Println("  (none)")
			}

			cmd.Printf("inbox (queued): %d\n", len(inbox))
			for _, e := range inbox {
				cmd.Printf("  - #%s %s\n", toString(e["id"]), toString(e["kind"]))
			}

			cmd.Printf("items: %d\n", len(items))
			for _, it := range items {
				note := toString(it["note"])
				if note != "" {
					note = " — " + note
				}
				cmd.Printf("  - %s:%s [%s]%s\n", toString(it["kind"]), toString(it["ref"]),
					toString(it["state"]), note)
			}
			return nil
		},
	}
}

func newAgentRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Удалить роль из реестра (файлы роли остаются на диске)",
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
	return newAgentToggleCmd("enable", "Включить роль")
}

func newAgentDisableCmd() *cobra.Command {
	return newAgentToggleCmd("disable", "Выключить роль")
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

func newAgentWakeCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "wake <id> [\"текст\"]",
		Short: "Положить событие в инбокс роли (пинг/сообщение)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: "usage: rocket agent wake <id> [\"текст\"]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"from": os.Getenv("ROCKET_SESSION_ID")}
			if len(args) == 2 {
				reqBody["text"] = args[1]
			}
			if kind != "" {
				reqBody["kind"] = kind
			}

			var resp map[string]any
			if err := c.Post(apiPath("v1", "agents", args[0], "wake"), reqBody, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("queued %s event #%s for %s\n", toString(resp["kind"]), toString(resp["event_id"]), args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "вид события инбокса (по умолчанию message)")
	return cmd
}

func newAgentStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Досье роли: состояние отслеживаемых issue/задач/пингов",
	}
	cmd.AddCommand(newAgentStateSetCmd())
	cmd.AddCommand(newAgentStateLsCmd())
	return cmd
}

// resolveRole picks the role a `state` command operates on: the explicit
// --agent flag, or the role of the calling instance (session id
// "<role>-run-<n>", see docs/10-agents.md).
func resolveRole(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if role := roleFromSessionID(os.Getenv("ROCKET_SESSION_ID")); role != "" {
		return role, nil
	}
	return "", &usageError{message: "role is unknown: pass --agent <id> (outside a role instance)"}
}

func newAgentStateSetCmd() *cobra.Command {
	var note, until, agent string
	var taskID int64

	cmd := &cobra.Command{
		Use:   "set <kind>:<ref> <state>",
		Short: "Записать состояние элемента досье",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket agent state set <kind>:<ref> <state> [--note \"...\"] [--until YYYY-MM-DD] [--task <id>] [--agent <role>]"}
			}

			kind, ref, err := splitItemRef(args[0])
			if err != nil {
				return &usageError{message: err.Error()}
			}
			snooze, err := parseUntil(until)
			if err != nil {
				return &usageError{message: err.Error()}
			}
			role, err := resolveRole(agent)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"kind": kind, "ref": ref, "state": args[1]}
			if note != "" {
				reqBody["note"] = note
			}
			if snooze != 0 {
				reqBody["snooze_until"] = snooze
			}
			if taskID != 0 {
				reqBody["task_id"] = taskID
			}

			var resp map[string]any
			if err := c.Put(apiPath("v1", "agents", role, "items"), reqBody, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%s:%s\t%s\n", toString(resp["kind"]), toString(resp["ref"]), toString(resp["state"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "почему элемент в этом состоянии")
	cmd.Flags().StringVar(&until, "until", "", "срок для deferred (YYYY-MM-DD или RFC3339)")
	cmd.Flags().Int64Var(&taskID, "task", 0, "id задачи rocket, связанной с элементом")
	cmd.Flags().StringVar(&agent, "agent", "", "роль (по умолчанию — роль текущего инстанса)")
	return cmd
}

func newAgentStateLsCmd() *cobra.Command {
	var state, agent string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Показать досье роли",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket agent state ls [--state <s>] [--agent <role>]"}
			}

			role, err := resolveRole(agent)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := apiPath("v1", "agents", role, "items")
			if state != "" {
				path += "?state=" + state
			}
			var items []map[string]any
			if err := c.Get(path, nil, &items); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, items)
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = tw.Write([]byte("KIND\tREF\tSTATE\tTASK\tUNTIL\tNOTE\n"))
			for _, it := range items {
				_, _ = tw.Write([]byte(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\n",
					toString(it["kind"]), toString(it["ref"]), toString(it["state"]),
					dashIfZero(it["task_id"]), formatUnix(it["snooze_until"]),
					dashIfEmpty(toString(it["note"])))))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "фильтр по состоянию (например deferred)")
	cmd.Flags().StringVar(&agent, "agent", "", "роль (по умолчанию — роль текущего инстанса)")
	return cmd
}

// questionsCell renders a role's open-thread counters as "open" or, when some
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

// formatUnix renders a JSON number of unix seconds as a date, "-" for zero.
func formatUnix(v any) string {
	s := toString(v)
	if s == "" || s == "0" {
		return "-"
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return s
	}
	return time.Unix(n, 0).Format("2006-01-02")
}

func labelsSuffix(v any) string {
	labels, ok := v.([]any)
	if !ok || len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		parts = append(parts, toString(l))
	}
	return " labels=" + strings.Join(parts, "|")
}

func mentionSuffix(v any) string {
	if b, ok := v.(bool); ok && b {
		return " mention-only"
	}
	return ""
}
