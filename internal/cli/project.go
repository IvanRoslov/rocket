package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Управление реестром проектов",
	}
	cmd.AddCommand(newProjectCreateCmd())
	cmd.AddCommand(newProjectLsCmd())
	cmd.AddCommand(newProjectShowCmd())
	cmd.AddCommand(newProjectLinkCmd())
	cmd.AddCommand(newProjectUnlinkCmd())
	cmd.AddCommand(newProjectRmCmd())
	return cmd
}

func newProjectCreateCmd() *cobra.Command {
	var main string
	var links []string
	var name string
	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Зарегистрировать проект",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket project create <id> --main <repo> [--link <repo>]... [--name \"...\"]"}
			}
			if main == "" {
				return &usageError{message: "usage: rocket project create <id> --main <repo> [--link <repo>]... [--name \"...\"]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			reqBody := map[string]any{
				"id":   args[0],
				"main": main,
			}
			if name != "" {
				reqBody["name"] = name
			}
			if len(links) > 0 {
				reqBody["linked"] = links
			}

			var resp map[string]any
			if err := c.Post("/v1/projects", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%v\t%v\n", resp["id"], resp["main"])
			return nil
		},
	}
	cmd.Flags().StringVar(&main, "main", "", "id главного репозитория (обязательно)")
	cmd.Flags().StringArrayVar(&links, "link", nil, "id связанного репозитория (можно несколько раз)")
	cmd.Flags().StringVar(&name, "name", "", "человекочитаемое имя проекта")
	return cmd
}

func newProjectLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Список зарегистрированных проектов",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket project ls"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var projects []map[string]any
			if err := c.Get("/v1/projects", nil, &projects); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, projects)
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = tw.Write([]byte("ID\tNAME\tMAIN\tLINKED\tSESSIONS\n"))
			for _, p := range projects {
				linked := joinLinked(p["linked"])
				_, _ = tw.Write([]byte(fmt.Sprintf(
					"%s\t%s\t%s\t%s\t%s\n",
					toString(p["id"]), toString(p["name"]), toString(p["main"]), linked, toString(p["live_sessions"]),
				)))
			}
			return tw.Flush()
		},
	}
}

func newProjectShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Показать детали проекта",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket project show <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Get(apiPath("v1", "projects", args[0]), nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}

			cmd.Printf("id:    %s\n", toString(resp["id"]))
			cmd.Printf("name:  %s\n", toString(resp["name"]))

			if main, ok := resp["main"].(map[string]any); ok {
				cmd.Printf("main:  %s (%s)\n", toString(main["id"]), toString(main["path"]))
			}

			cmd.Println("linked:")
			if linked, ok := resp["linked"].([]any); ok && len(linked) > 0 {
				for _, l := range linked {
					if lr, ok := l.(map[string]any); ok {
						cmd.Printf("  - %s (%s)\n", toString(lr["id"]), toString(lr["path"]))
					}
				}
			} else {
				cmd.Println("  (none)")
			}

			cmd.Printf("live sessions: %s\n", toString(resp["live_sessions"]))
			return nil
		},
	}
}

func newProjectLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <project> <repo>",
		Short: "Добавить связанный репозиторий к проекту",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket project link <project> <repo>"}
			}
			return updateLinked(cmd, args[0], args[1], true)
		},
	}
}

func newProjectUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <project> <repo>",
		Short: "Убрать связанный репозиторий из проекта",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return &usageError{message: "usage: rocket project unlink <project> <repo>"}
			}
			return updateLinked(cmd, args[0], args[1], false)
		},
	}
}

// updateLinked implements `project link`/`project unlink` by fetching the
// project's current linked list via GET, mutating it, and sending the
// result via PATCH.
func updateLinked(cmd *cobra.Command, project, repo string, add bool) error {
	c, _, err := connect(true)
	if err != nil {
		return err
	}

	var detail map[string]any
	if err := c.Get(apiPath("v1", "projects", project), nil, &detail); err != nil {
		return err
	}

	var current []string
	if linked, ok := detail["linked"].([]any); ok {
		for _, l := range linked {
			if lr, ok := l.(map[string]any); ok {
				current = append(current, toString(lr["id"]))
			}
		}
	}

	var updated []string
	if add {
		found := false
		for _, id := range current {
			if id == repo {
				found = true
			}
		}
		updated = current
		if !found {
			updated = append(updated, repo)
		}
	} else {
		found := false
		for _, id := range current {
			if id == repo {
				found = true
				continue
			}
			updated = append(updated, id)
		}
		if !found {
			return &usageError{message: fmt.Sprintf("repo %q is not linked to project %q", repo, project)}
		}
	}

	var resp map[string]any
	if err := c.Patch(apiPath("v1", "projects", project), map[string]any{"linked": updated}, &resp); err != nil {
		return err
	}

	if flags.JSON {
		return printJSON(cmd, resp)
	}
	if add {
		cmd.Printf("linked %s to %s\n", repo, project)
	} else {
		cmd.Printf("unlinked %s from %s\n", repo, project)
	}
	return nil
}

func newProjectRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Удалить проект из реестра",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket project rm <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Delete(apiPath("v1", "projects", args[0]), nil, &resp); err != nil {
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

func joinLinked(v any) string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return "-"
	}
	out := ""
	for i, item := range arr {
		if i > 0 {
			out += ","
		}
		out += toString(item)
	}
	return out
}
