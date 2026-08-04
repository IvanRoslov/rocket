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
	Participants []string             `json:"participants,omitempty"`
	WaitingOn    []string             `json:"waiting_on,omitempty"`
	YourTurn     bool                 `json:"your_turn,omitempty"`
	WhoseTurn    string               `json:"whose_turn,omitempty"`
	AskedAt      int64                `json:"asked_at"`
	ResolvedAt   int64                `json:"resolved_at,omitempty"`
	Messages     []questionMessageRow `json:"messages"`
}

// newAgentAskCmd builds "rocket agent ask": open a Q&A thread with a role.
// The direction is decided by the daemon from the calling session, so one
// command serves both: run by a human it asks the role (and wakes it); run
// inside a role instance it escalates to the human.
func newAgentAskCmd() *cobra.Command {
	var context string
	var to []string

	cmd := &cobra.Command{
		Use:   "ask <role> \"<вопрос>\"",
		Short: "Открыть тред-вопрос с ролью (направление — по вызывающему)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket agent ask <role> \"<вопрос>\" [--context <md>] [--to <id,...>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"body": args[1]}
			if context != "" {
				reqBody["context"] = context
			}
			setTo(reqBody, parseTo(to))

			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agents", args[0], "questions"), reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("question Q%d (#%d) opened for %s\n", resp.Ordinal, resp.ID, args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&context, "context", "", "дополнительный контекст (MD)")
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
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
	var to []string

	cmd := &cobra.Command{
		Use:   "reply <question-id>|<role>/Q<n> \"<текст>\"",
		Short: "Ответить в тред роли",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket agent reply <question-id>|<role>/Q<n> \"<текст>\" [--to <id,...>]"}
			}
			id, err := resolveAgentQuestionRef(args[0])
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{"body": args[1]}
			setTo(reqBody, parseTo(to))

			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agent-questions", id, "reply"),
				reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("reply added to Q%d (#%d)\n", resp.Ordinal, resp.ID)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
	return cmd
}

// newAgentAnswerCmd builds "rocket agent answer": only the human closes a
// role thread, with an answer or by dismissing it.
func newAgentAnswerCmd() *cobra.Command {
	var dismiss bool
	var to []string

	cmd := &cobra.Command{
		Use:   "answer <question-id>|<role>/Q<n> [\"<ответ>\"]",
		Short: "Закрыть тред роли ответом или без ответа",
		RunE: func(cmd *cobra.Command, args []string) error {
			usage := &usageError{message: "usage: rocket agent answer <question-id>|<role>/Q<n> \"<ответ>\" | --dismiss (exactly one)"}
			if len(args) < 1 || len(args) > 2 {
				return usage
			}

			hasBody := len(args) == 2
			if hasBody == dismiss {
				return usage
			}

			id, err := resolveAgentQuestionRef(args[0])
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{}
			if dismiss {
				reqBody["dismiss"] = true
			} else {
				reqBody["body"] = args[1]
			}
			setTo(reqBody, parseTo(to))

			var resp agentQuestionRow
			if err := c.Post(apiPath("v1", "agent-questions", id, "answer"), reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			if dismiss {
				cmd.Printf("question Q%d (#%d) dismissed\n", resp.Ordinal, resp.ID)
			} else {
				cmd.Printf("question Q%d (#%d) answered\n", resp.Ordinal, resp.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dismiss, "dismiss", false, "закрыть тред без ответа")
	cmd.Flags().StringSliceVar(&to, "to", nil, toFlagUsage)
	return cmd
}

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
		fmt.Fprintf(&sb, "Q%d (#%d) [%s]%s\n", q.Ordinal, q.ID, q.Status,
			threadTurnArrow(q.WaitingOn, q.YourTurn))
		fmt.Fprintf(&sb, "  %s\n", q.Body)
		if q.Context != "" {
			fmt.Fprintf(&sb, "  context: %s\n", q.Context)
		}
		renderParticipantsLine(&sb, q.Participants)
		for _, m := range q.Messages {
			renderThreadMessage(&sb, m)
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
