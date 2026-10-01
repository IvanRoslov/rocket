package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// gateRow mirrors internal/api.gateResponse: one storm exit gate (task #4901).
type gateRow struct {
	ID          int64  `json:"id"`
	TaskID      int64  `json:"task_id"`
	SpecVersion int64  `json:"spec_version"`
	PlanVersion *int64 `json:"plan_version"`
	Status      string `json:"status"`
	Comment     string `json:"comment"`
	DecidedBy   string `json:"decided_by"`
	RequestedBy string `json:"requested_by"`
	RequestedAt int64  `json:"requested_at"`
	DecidedAt   *int64 `json:"decided_at"`
}

// newTaskGateCmd builds "rocket task gate": the storm exit. The orchestrator
// requests a gate once spec (and plan) are written; the human decides it
// from their own terminal (or the dashboard) with go / changes.
func newTaskGateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Гейт выхода из шторма: запрос и решение Go / «Нужны правки»",
	}
	cmd.AddCommand(newTaskGateRequestCmd())
	cmd.AddCommand(newTaskGateLsCmd())
	cmd.AddCommand(newTaskGateGoCmd())
	cmd.AddCommand(newTaskGateChangesCmd())
	return cmd
}

func parseIDArg(arg, what string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil || id <= 0 {
		return 0, &usageError{message: "invalid " + what + " id"}
	}
	return id, nil
}

func newTaskGateRequestCmd() *cobra.Command {
	const usage = "usage: rocket task gate request <task-id>"
	return &cobra.Command{
		Use:   "request <task-id>",
		Short: "Запросить Go по последним версиям спеки и плана",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: usage}
			}
			taskID, err := parseIDArg(args[0], "task")
			if err != nil {
				return err
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var resp gateRow
			if err := c.Post(apiPath("v1", "tasks", strconv.FormatInt(taskID, 10), "gates"), map[string]any{}, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("gate #%d requested: %s — ждём решения человека\n", resp.ID, gateVersions(resp))
			return nil
		},
	}
}

func newTaskGateLsCmd() *cobra.Command {
	const usage = "usage: rocket task gate ls <task-id>"
	return &cobra.Command{
		Use:   "ls <task-id>",
		Short: "История гейтов задачи (новые первыми)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: usage}
			}
			taskID, err := parseIDArg(args[0], "task")
			if err != nil {
				return err
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var resp struct {
				Gates []gateRow `json:"gates"`
			}
			if err := c.Get(apiPath("v1", "tasks", strconv.FormatInt(taskID, 10), "gates"), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderGates(taskID, resp.Gates))
			return nil
		},
	}
}

func newTaskGateGoCmd() *cobra.Command {
	const usage = "usage: rocket task gate go <gate-id>"
	return &cobra.Command{
		Use:   "go <gate-id>",
		Short: "Go: спека принята, задача уходит в in_progress (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: usage}
			}
			gateID, err := parseIDArg(args[0], "gate")
			if err != nil {
				return err
			}
			return decideGate(cmd, gateID, "go", "")
		},
	}
}

func newTaskGateChangesCmd() *cobra.Command {
	var file string
	const usage = "usage: rocket task gate changes <gate-id> \"<комментарий>\" | --file <path>"
	cmd := &cobra.Command{
		Use:   "changes <gate-id> [\"<комментарий>\"]",
		Short: "Нужны правки: комментарий уходит оркестратору (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return &usageError{message: usage}
			}
			gateID, err := parseIDArg(args[0], "gate")
			if err != nil {
				return err
			}
			comment, err := textBody(cmd, argAt(args, 1), len(args) == 2, file, usage)
			if err != nil {
				return err
			}
			if strings.TrimSpace(comment) == "" {
				return &usageError{message: usage}
			}
			return decideGate(cmd, gateID, "changes", comment)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "файл с комментарием ('-' — stdin)")
	return cmd
}

func decideGate(cmd *cobra.Command, gateID int64, decision, comment string) error {
	c, _, err := connect(true)
	if err != nil {
		return err
	}
	var resp gateRow
	body := map[string]any{"decision": decision, "comment": comment}
	if err := c.Post(apiPath("v1", "gates", strconv.FormatInt(gateID, 10), "decide"), body, &resp); err != nil {
		return err
	}
	if flags.JSON {
		return printJSON(cmd, resp)
	}
	cmd.Print(gateDecisionLine(resp))
	return nil
}

// gateVersions renders "спека vN · план vM" ("план —" without a plan).
func gateVersions(g gateRow) string {
	plan := "—"
	if g.PlanVersion != nil {
		plan = fmt.Sprintf("v%d", *g.PlanVersion)
	}
	return fmt.Sprintf("спека v%d · план %s", g.SpecVersion, plan)
}

func gateDecisionLine(g gateRow) string {
	line := fmt.Sprintf("gate #%d: %s (%s)", g.ID, g.Status, gateVersions(g))
	if g.Comment != "" {
		line += ": " + g.Comment
	}
	return line + "\n"
}

func renderGates(taskID int64, gates []gateRow) string {
	if len(gates) == 0 {
		return fmt.Sprintf("task #%d: гейтов нет\n", taskID)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "task #%d — гейты (новые первыми):\n", taskID)
	for _, g := range gates {
		fmt.Fprintf(&sb, "  #%d  %s  %s", g.ID, gateVersions(g), g.Status)
		if g.Comment != "" {
			sb.WriteString(": " + g.Comment)
		}
		at := g.RequestedAt
		if g.DecidedAt != nil {
			at = *g.DecidedAt
		}
		fmt.Fprintf(&sb, "  (%s)\n", time.Unix(at, 0).Local().Format("2006-01-02 15:04"))
	}
	return sb.String()
}
