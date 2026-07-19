package cli

import (
	"github.com/spf13/cobra"
)

func newSpawnCmd() *cobra.Command {
	var repo, task, prompt, agentName string
	var subtaskID int64

	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Запустить воркера для подзадачи (вызывается оркестратором)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" || task == "" {
				return &usageError{message: "usage: rocket spawn --repo <id> --task <name> [--prompt <text>] [--agent <name>] [--subtask <id>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"repo": repo,
				"task": task,
			}
			if prompt != "" {
				reqBody["prompt"] = prompt
			}
			if agentName != "" {
				reqBody["agent"] = agentName
			}
			if subtaskID != 0 {
				reqBody["subtask_id"] = subtaskID
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
			if resp["subtask_id"] != nil {
				cmd.Printf("subtask #%v\n", resp["subtask_id"])
			}
			cmd.Printf("attach: rocket attach %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "id репозитория (обязательно)")
	cmd.Flags().StringVar(&task, "task", "", "имя задачи (обязательно)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "первое сообщение агенту")
	cmd.Flags().StringVar(&agentName, "agent", "", "имя агента (по умолчанию — из конфига)")
	cmd.Flags().Int64Var(&subtaskID, "subtask", 0, "id существующей подзадачи для привязки (иначе создаётся новая)")

	return cmd
}
