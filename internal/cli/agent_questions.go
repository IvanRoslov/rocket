package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// agentQuestionRow is the JSON shape of a role's Q&A thread, mirroring
// internal/api.agentQuestionResponse. Thread entries reuse questionMessageRow
// — role and task threads have the same message shape.
type agentQuestionRow struct {
	ID         int64  `json:"id"`
	RoleID     string `json:"role_id"`
	Ordinal    int    `json:"ordinal"`
	AskedBy    string `json:"asked_by"`
	Body       string `json:"body"`
	Context    string `json:"context,omitempty"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	// Participants, WaitingOn and YourTurn mirror questionRow; WhoseTurn is
	// the pre-participant field the CLI no longer prints but keeps on the wire
	// for web and mobile — subtask #736 retires it.
	Participants []string `json:"participants,omitempty"`
	WaitingOn    []string `json:"waiting_on,omitempty"`
	YourTurn     bool     `json:"your_turn,omitempty"`
	WhoseTurn    string   `json:"whose_turn,omitempty"`
	// Attention, Type, Options, LocalRef, Echo and DryRun mirror questionRow.
	Attention  []string             `json:"attention,omitempty"`
	Type       string               `json:"type,omitempty"`
	Options    []string             `json:"options,omitempty"`
	LocalRef   string               `json:"local_ref,omitempty"`
	AskedAt    int64                `json:"asked_at"`
	ResolvedAt int64                `json:"resolved_at,omitempty"`
	Messages   []questionMessageRow `json:"messages"`
	Echo       string               `json:"echo,omitempty"`
	DryRun     bool                 `json:"dry_run,omitempty"`
}

// newAgentAskCmd builds "rocket agent ask": open a Q&A thread with a role.
// The direction is decided by the daemon from the calling session, so one
// command serves both: run by a human it asks the role (and wakes it); run
// inside a role instance it escalates to the human.
func newAgentAskCmd() *cobra.Command {
	var context string
	var to []string
	var file string
	var options []string
	var fyi bool

	const usage = "usage: rocket agent ask <role> \"<вопрос>\" | --file <path> [--context <md>] [--to <id,...>] [--option <текст>]... [--fyi]"

	cmd := &cobra.Command{
		Use:   "ask <role> [\"<вопрос>\"]",
		Short: "Открыть тред-вопрос с ролью (направление — по вызывающему)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			if err := validateAskFlags(options, fyi, usage); err != nil {
				return err
			}

			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := askRequestBody(body, context, parseTo(to), options, fyi)

			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agents", args[0], "questions"), reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("тред %s открыт\n", resp.ref())
			return nil
		},
	}
	cmd.Flags().StringVar(&context, "context", "", "дополнительный контекст (MD)")
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с вопросом ('-' — stdin)")
	cmd.Flags().StringArrayVar(&options, "option", nil, optionFlagUsage)
	cmd.Flags().BoolVar(&fyi, "fyi", false, fyiFlagUsage)
	return cmd
}

func newAgentQuestionsCmd() *cobra.Command {
	var openOnly bool

	cmd := &cobra.Command{
		Use:   "questions [<role>]",
		Short: "Показать треды агента",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return &usageError{message: "usage: rocket agent questions [<role>] [--open]"}
			}

			role := ""
			if len(args) == 1 {
				role = args[0]
			}
			role, err := resolveAgentID(role)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			path := apiPath("v1", "agents", role, "questions")
			if openOnly {
				path += "?status=open"
			}
			var resp struct {
				Questions []agentQuestionRow `json:"questions"`
			}
			if err := c.Get(path, nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp.Questions)
			}
			cmd.Print(renderAgentQuestions(role, resp.Questions))
			return nil
		},
	}
	cmd.Flags().BoolVar(&openOnly, "open", false, "показать только открытые треды")
	return cmd
}

// newAgentReplyCmd builds "rocket agent reply": a thread entry from either
// side. A role instance's reply into a resolved thread reopens it.
func newAgentReplyCmd() *cobra.Command {
	var opts threadReplyOptions
	var file string

	const usage = "usage: rocket agent reply <role>/Q<n>|<question-id> \"<текст>\" | --file <path> " +
		"[--to <id,...>] [--dry-run] [--join]"

	cmd := &cobra.Command{
		Use:   "reply <role>/Q<n>|<question-id> [\"<текст>\"]",
		Short: "Ответить в тред роли",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			body, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			id, err := resolveAgentQuestionRef(args[0])
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			opts.body = body
			opts.to = parseTo(opts.to)
			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agent-questions", id, "reply"),
				opts.requestBody(), &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderWriteResult("реплика добавлена в", resp))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&opts.to, "to", nil, toFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с текстом ('-' — stdin)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, dryRunFlagUsage)
	cmd.Flags().BoolVar(&opts.join, "join", false, joinFlagUsage)
	return cmd
}

// newAgentCloseCmd builds "rocket agent close": the role-thread twin of
// "rocket task close" — one verb ending a thread with an answer, a choice
// among its options, or as no longer relevant. hidden builds it under its
// pre-#1023 name "answer", kept working but out of the help.
func newAgentCloseCmd(hidden bool) *cobra.Command {
	var opts threadCloseOptions
	var file string

	name := "close"
	if hidden {
		name = "answer"
	}

	usage := "usage: rocket agent " + name + " <role>/Q<n>|<question-id> \"<резолюция>\" | --file <path> | " +
		"--choose <n> | --dismiss [\"<почему>\"] (ровно одно) [--to <id,...>] [--dry-run] [--join]"

	cmd := &cobra.Command{
		Use:    name + " <role>/Q<n>|<question-id> [\"<резолюция>\"]",
		Short:  "Закрыть тред роли: ответом, выбором варианта или как неактуальный",
		Hidden: hidden,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}

			hasBody := len(args) == 2
			if hasBody || file != "" {
				body, err := textBody(cmd, argAt(args, 1), hasBody, file, usage)
				if err != nil {
					return err
				}
				opts.body = body
			}
			if err := opts.validate(usage); err != nil {
				return err
			}

			id, err := resolveAgentQuestionRef(args[0])
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			opts.to = parseTo(opts.to)
			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agent-questions", id, "answer"),
				opts.requestBody(), &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderWriteResult(closeAction(opts), resp))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.dismiss, "dismiss", false, dismissFlagUsage)
	cmd.Flags().IntVar(&opts.choose, "choose", 0, chooseFlagUsage)
	cmd.Flags().StringSliceVar(&opts.to, "to", nil, toFlagUsage)
	cmd.Flags().StringVar(&file, "file", "", "файл с резолюцией ('-' — stdin)")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, dryRunFlagUsage)
	cmd.Flags().BoolVar(&opts.join, "join", false, joinFlagUsage)
	return cmd
}

// newAgentAnswerCmd is the pre-#1023 name of "agent close", kept working and
// hidden from help.
func newAgentAnswerCmd() *cobra.Command { return newAgentCloseCmd(true) }

// renderAgentQuestions renders a role's threads: an "agent <role>" header
// followed by, per thread, a header line "Q<ordinal> (#<id>) [status] <arrow>"
// (arrow names who is awaited, empty when nobody is), the indented body, an
// optional context line, an optional participants line, and indented thread
// lines ("  [user] ..." / "  [<session>] ..."). Mirrors renderQuestions for
// tasks, down to the shared helpers. Returns "" if qs is empty.
func renderAgentQuestions(role string, qs []agentQuestionRow) string {
	if len(qs) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "agent %s\n", role)
	for _, q := range qs {
		fmt.Fprintf(&sb, "%s [%s]%s\n",
			threadRef(q.LocalRef, role, q.Ordinal),
			threadStatusLabel(q.Status, q.Type),
			threadTurnArrow(q.WaitingOn, q.YourTurn))
		fmt.Fprintf(&sb, "  %s\n", q.Body)
		if q.Context != "" {
			fmt.Fprintf(&sb, "  context: %s\n", q.Context)
		}
		renderThreadOptions(&sb, q.Options)
		renderParticipantsLine(&sb, q.Participants)
		for _, m := range q.Messages {
			renderThreadMessage(&sb, m)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
