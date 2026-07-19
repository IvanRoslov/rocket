package cli

import (
	"github.com/spf13/cobra"
)

func newGithubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Управление интеграцией с GitHub",
	}
	cmd.AddCommand(newGithubAuthCmd())
	return cmd
}

func newGithubAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth <token>",
		Short: "Сохранить и проверить GitHub-токен",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket github auth <token>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Put("/v1/settings", map[string]string{"github_token": args[0]}, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("authenticated as %v\n", resp["login"])
			return nil
		},
	}
}
