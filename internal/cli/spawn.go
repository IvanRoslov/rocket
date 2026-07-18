package cli

import (
	"github.com/spf13/cobra"
)

func newSpawnCmd() *cobra.Command {
	var project, repo, task, feature, prompt, agentName string

	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Запустить новую сессию воркера",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" || repo == "" || task == "" {
				return &usageError{message: "usage: rocket spawn --project <id> --repo <id> --task <name> [--feature <slug>] [--prompt <text>] [--agent <name>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"project": project,
				"repo":    repo,
				"task":    task,
			}
			if feature != "" {
				reqBody["feature"] = feature
			}
			if prompt != "" {
				reqBody["prompt"] = prompt
			}
			if agentName != "" {
				reqBody["agent"] = agentName
			}

			var resp map[string]any
			if err := c.Post("/v1/sessions", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}

			id := toString(resp["id"])
			cmd.Printf("session %s\n", id)
			cmd.Printf("branch %s\n", toString(resp["branch"]))
			cmd.Printf("worktree %s\n", toString(resp["worktree_path"]))
			cmd.Printf("attach: rocket attach %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "id проекта (обязательно)")
	cmd.Flags().StringVar(&repo, "repo", "", "id репозитория (обязательно)")
	cmd.Flags().StringVar(&task, "task", "", "имя задачи (обязательно)")
	cmd.Flags().StringVar(&feature, "feature", "", "slug фичи (по умолчанию = task)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "первое сообщение агенту")
	cmd.Flags().StringVar(&agentName, "agent", "", "имя агента (по умолчанию — из конфига)")

	return cmd
}
