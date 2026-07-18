package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// newUpCmd implements `rocket up "<описание>"`: creates a root task (with
// the same project-defaulting behavior as `task add`) and immediately
// starts it, spawning its orchestrator in one step.
func newUpCmd() *cobra.Command {
	var projectID string
	var agentName string
	var description string
	var descFile string

	cmd := &cobra.Command{
		Use:   "up \"<описание>\"",
		Short: "Создать и сразу запустить задачу (add + start)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket up \"<описание>\" [--project <id>] [--agent <name>] [--desc <md> | --desc-file <f>]"}
			}

			if description != "" && descFile != "" {
				return &usageError{message: "--desc and --desc-file are mutually exclusive"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			// If project not specified, try to get default (same behavior
			// as `rocket task add`).
			if projectID == "" {
				var projects []map[string]any
				if err := c.Get("/v1/projects", nil, &projects); err != nil {
					return err
				}
				if len(projects) == 1 {
					projectID = projects[0]["id"].(string)
				} else if len(projects) == 0 {
					return &usageError{message: "no projects found; specify --project"}
				} else {
					return &usageError{message: "multiple projects found; specify --project <id>"}
				}
			}

			if descFile != "" {
				data, err := os.ReadFile(descFile)
				if err != nil {
					return fmt.Errorf("failed to read description file: %w", err)
				}
				description = string(data)
			}

			addReqBody := map[string]any{
				"title":   args[0],
				"project": projectID,
			}
			if description != "" {
				addReqBody["description"] = description
			}

			var addResp taskRow
			if err := c.Post("/v1/tasks", addReqBody, &addResp); err != nil {
				return err
			}

			var startReqBody map[string]any
			if agentName != "" {
				startReqBody = map[string]any{"agent": agentName}
			}

			startPath := apiPath("v1", "tasks", fmt.Sprint(addResp.ID), "start")
			var startResp taskStartResponse
			if err := c.Post(startPath, startReqBody, &startResp); err != nil {
				// Start failed after task creation. Print task ID and error so the user
				// can retry with: rocket task start <id>
				if flags.JSON {
					return printJSON(cmd, map[string]any{
						"task_id": addResp.ID,
						"error":   err.Error(),
					})
				}
				cmd.Printf("TASK=#%d created (start failed: retry with: rocket task start %d)\n", addResp.ID, addResp.ID)
				return err
			}

			if flags.JSON {
				return printJSON(cmd, map[string]any{
					"task_id":      startResp.TaskID,
					"feature_slug": startResp.FeatureSlug,
					"session_id":   startResp.SessionID,
				})
			}
			cmd.Printf("TASK=#%d\n", startResp.TaskID)
			cmd.Printf("SLUG=%s\n", startResp.FeatureSlug)
			cmd.Printf("SESSION=%s\n", startResp.SessionID)
			cmd.Printf("attach: rocket attach %s\n", startResp.SessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "id проекта")
	cmd.Flags().StringVar(&agentName, "agent", "", "имя агента (по умолчанию — из конфига)")
	cmd.Flags().StringVar(&description, "desc", "", "описание задачи (MD)")
	cmd.Flags().StringVar(&descFile, "desc-file", "", "файл с описанием задачи (MD)")

	return cmd
}
