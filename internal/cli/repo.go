package cli

import (
	"encoding/json"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Управление реестром репозиториев",
	}
	cmd.AddCommand(newRepoAddCmd())
	cmd.AddCommand(newRepoLsCmd())
	cmd.AddCommand(newRepoRmCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Зарегистрировать репозиторий",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket repo add <path> [--id <id>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]string{"path": args[0]}
			if id != "" {
				reqBody["id"] = id
			}

			var resp map[string]any
			if err := c.Post("/v1/repos", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%v\t%v\n", resp["id"], resp["path"])
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "явный id репозитория")
	return cmd
}

func newRepoLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Список зарегистрированных репозиториев",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket repo ls"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var repos []map[string]any
			if err := c.Get("/v1/repos", nil, &repos); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, repos)
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = tw.Write([]byte("ID\tPATH\tBRANCH\n"))
			for _, r := range repos {
				_, _ = tw.Write([]byte(
					toString(r["id"]) + "\t" + toString(r["path"]) + "\t" + toString(r["default_branch"]) + "\n",
				))
			}
			return tw.Flush()
		},
	}
}

func newRepoRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Удалить репозиторий из реестра",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket repo rm <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Delete(apiPath("v1", "repos", args[0]), nil, &resp); err != nil {
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

func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
