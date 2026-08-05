package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

// Milestones (task #1023, spec v2) are the tasks a persistent agent takes on
// itself. This file holds the two commands that move one between agents and
// the rendering shared by the task card and the agent card.

// newTaskTakeCmd builds "rocket task take": the agent picks up an unassigned
// milestone from its own session. The daemon identifies the agent from
// $ROCKET_SESSION_ID, so there is nothing to name on the command line.
func newTaskTakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "take <id>",
		Short: "Взять майлстон (из сессии постоянного агента)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket task take <id>"}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp taskRow
			if err := c.Post(apiPath("v1", "tasks", args[0], "take"), nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("майлстон #%d взят: %s\n", resp.ID, resp.AssignedRole)
			return nil
		},
	}
}

// assignRequestBody builds the POST /v1/tasks/{id}/assign body, refusing the
// two ambiguous calls: neither an agent nor --none, or both at once.
func assignRequestBody(agentID string, none bool) (map[string]any, error) {
	if none == (agentID != "") {
		return nil, &usageError{message: "usage: rocket task assign <id> <agent-id> | --none"}
	}
	if none {
		return map[string]any{"none": true}, nil
	}
	return map[string]any{"agent_id": agentID}, nil
}

// newTaskAssignCmd builds "rocket task assign": the human hands a milestone to
// an agent, or takes it back with --none.
func newTaskAssignCmd() *cobra.Command {
	var none bool

	const usage = "usage: rocket task assign <id> <agent-id> | --none"

	cmd := &cobra.Command{
		Use:   "assign <id> <agent-id>",
		Short: "Назначить майлстон агенту (или снять с --none)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}
			reqBody, err := assignRequestBody(argAt(args, 1), none)
			if err != nil {
				return err
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp taskRow
			if err := c.Post(apiPath("v1", "tasks", args[0], "assign"), reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			if resp.AssignedRole == "" {
				cmd.Printf("майлстон #%d снят с агента\n", resp.ID)
			} else {
				cmd.Printf("майлстон #%d назначен: %s\n", resp.ID, resp.AssignedRole)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&none, "none", false, "снять майлстон с агента")
	return cmd
}

// renderMilestoneInfo writes the milestone lines of a task card: who holds it
// and how to get into that agent's session (one session serves all its
// milestones, hence `rocket agent attach`, not a per-task attach).
func renderMilestoneInfo(w io.Writer, assignedRole string) {
	fmt.Fprintf(w, "Milestone: yes\n")
	if assignedRole == "" {
		fmt.Fprintf(w, "Agent: не взят\n")
		return
	}
	fmt.Fprintf(w, "Agent: %s\n", assignedRole)
	fmt.Fprintf(w, "Attach: rocket agent attach %s\n", assignedRole)
}

// renderAgentMilestones writes the milestones block of an agent card from the
// raw `milestones` field of GET /v1/agents/{id}.
func renderAgentMilestones(w io.Writer, raw any) {
	items, _ := raw.([]any)
	fmt.Fprintf(w, "milestones: %d\n", len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(w, "  - #%s %s [%s]\n",
			toString(m["id"]), toString(m["title"]), toString(m["status"]))
	}
}
