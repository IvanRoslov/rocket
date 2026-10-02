package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/IvanRoslov/rocket/internal/client"
)

// modelsClient is the slice of *client.Client the model-profile commands
// use; tests bind it to a fake daemon on a unix socket.
type modelsClient interface {
	Get(path string, in, out any) error
	Post(path string, in, out any) error
	Patch(path string, in, out any) error
	Put(path string, in, out any) error
	Delete(path string, in, out any) error
}

// connectModels is the production modelsClient factory.
func connectModels() (modelsClient, error) {
	c, _, err := connect(true)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// modelProfileRow is a profile as GET /v1/model-profiles and
// /v1/model-profiles/available return it (the latter without
// enabled/position).
type modelProfileRow struct {
	Name        string `json:"name"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Position    int    `json:"position"`
}

func newModelsCmd() *cobra.Command {
	return newModelsCmdWith(connectModels)
}

// newModelsCmdWith builds `rocket models` with dial as its daemon
// connection (tests pass a fake).
func newModelsCmdWith(dial func() (modelsClient, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Профили моделей: какой агент, модель и усилие получает запуск",
	}
	cmd.AddCommand(newModelsLsCmd(dial))
	cmd.AddCommand(newModelsAddCmd(dial))
	cmd.AddCommand(newModelsEditCmd(dial))
	cmd.AddCommand(newModelsRmCmd(dial))
	cmd.AddCommand(newModelsDefaultCmd(dial))
	cmd.AddCommand(newModelsCatalogCmd(dial))
	cmd.AddCommand(newModelsImportCmd(dial))
	return cmd
}

func newModelsLsCmd(dial func() (modelsClient, error)) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Профили, доступные вызывающему (агенту — разрешённые его задаче; человеку — реестр)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket models ls [--all]"}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			inSession := os.Getenv("ROCKET_SESSION_ID") != ""
			if flags.JSON {
				return printModelsJSON(cmd, c, inSession, all)
			}
			return runModelsLs(c, cmd.OutOrStdout(), inSession, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "весь реестр, включая выключенные профили")
	return cmd
}

// availableReply is GET /v1/model-profiles/available.
type availableReply struct {
	Profiles []modelProfileRow `json:"profiles"`
	Default  string            `json:"default"`
}

func printModelsJSON(cmd *cobra.Command, c modelsClient, inSession, all bool) error {
	if inSession && !all {
		var av availableReply
		if err := c.Get("/v1/model-profiles/available", nil, &av); err != nil {
			return err
		}
		return printJSON(cmd, av)
	}
	var reg struct {
		Profiles []modelProfileRow `json:"profiles"`
	}
	if err := c.Get("/v1/model-profiles", nil, &reg); err != nil {
		return err
	}
	return printJSON(cmd, reg)
}

// runModelsLs prints the profiles the caller may use. An agent session
// (inSession) sees what the daemon allows its feature task, marking the one
// a spawn without --profile gets; the human sees the registry with the two
// defaults marked — enabled profiles only unless all, which also adds an
// ENABLED column. An agent session with all sees the whole registry too:
// reading it is not restricted, only launching from it is.
func runModelsLs(c modelsClient, w io.Writer, inSession, all bool) error {
	if inSession && !all {
		var av availableReply
		if err := c.Get("/v1/model-profiles/available", nil, &av); err != nil {
			return err
		}
		if len(av.Profiles) == 0 {
			fmt.Fprintln(w, "no model profiles are allowed for this task — ask the human (rocket models / dashboard)")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAGENT\tMODEL\tEFFORT\tDESCRIPTION")
		for _, p := range av.Profiles {
			name := p.Name
			if p.Name == av.Default {
				name += " *"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, p.Agent, dash(p.Model), dash(p.Effort), p.Description)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w, "\n* = default (what `rocket spawn` without --profile gets)")
		return nil
	}

	var reg struct {
		Profiles []modelProfileRow `json:"profiles"`
	}
	if err := c.Get("/v1/model-profiles", nil, &reg); err != nil {
		return err
	}
	var settings map[string]any
	if err := c.Get("/v1/settings", nil, &settings); err != nil {
		return err
	}
	defOrch := toString(settings["default_orchestrator_profile"])
	defWorker := toString(settings["default_worker_profile"])

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	header := "NAME\tAGENT\tMODEL\tEFFORT\tDEFAULT\tDESCRIPTION"
	if all {
		header = "NAME\tAGENT\tMODEL\tEFFORT\tENABLED\tDEFAULT\tDESCRIPTION"
	}
	fmt.Fprintln(tw, header)
	for _, p := range reg.Profiles {
		if !p.Enabled && !all {
			continue
		}
		var defs []string
		if p.Name == defOrch {
			defs = append(defs, "orchestrator")
		}
		if p.Name == defWorker {
			defs = append(defs, "worker")
		}
		def := dash(strings.Join(defs, ","))
		if all {
			enabled := "no"
			if p.Enabled {
				enabled = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Agent, dash(p.Model), dash(p.Effort), enabled, def, p.Description)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Agent, dash(p.Model), dash(p.Effort), def, p.Description)
		}
	}
	return tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newModelsAddCmd(dial func() (modelsClient, error)) *cobra.Command {
	var agentName, model, effort, description string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Добавить профиль (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || agentName == "" {
				return &usageError{message: "usage: rocket models add <name> --agent <agent> [--model M] [--effort E] [--description D]"}
			}
			body := map[string]any{"name": args[0], "agent": agentName}
			for flag, v := range map[string]string{"model": model, "effort": effort, "description": description} {
				if cmd.Flags().Changed(flag) {
					body[flag] = v
				}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var out modelProfileRow
			if err := c.Post("/v1/model-profiles", body, &out); err != nil {
				return explainHumanOnly(err)
			}
			if flags.JSON {
				return printJSON(cmd, out)
			}
			cmd.Printf("profile %s added\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "агент профиля: claude-code | codex (обязательно)")
	cmd.Flags().StringVar(&model, "model", "", "модель (пусто — модель агента по умолчанию)")
	cmd.Flags().StringVar(&effort, "effort", "", "уровень усилия (пусто — по умолчанию)")
	cmd.Flags().StringVar(&description, "description", "", "для чего подходит профиль — это читает оркестратор")
	return cmd
}

func newModelsEditCmd(dial func() (modelsClient, error)) *cobra.Command {
	var model, effort, description string
	var enable, disable bool
	var position int
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Изменить профиль (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			const usage = "usage: rocket models edit <name> [--model M] [--effort E] [--description D] [--enable|--disable] [--position N]"
			if len(args) != 1 || (enable && disable) {
				return &usageError{message: usage}
			}
			body := map[string]any{}
			for flag, v := range map[string]string{"model": model, "effort": effort, "description": description} {
				if cmd.Flags().Changed(flag) {
					body[flag] = v
				}
			}
			if enable {
				body["enabled"] = true
			}
			if disable {
				body["enabled"] = false
			}
			if cmd.Flags().Changed("position") {
				body["position"] = position
			}
			if len(body) == 0 {
				return &usageError{message: usage}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var out modelProfileRow
			if err := c.Patch(apiPath("v1", "model-profiles", args[0]), body, &out); err != nil {
				return explainHumanOnly(err)
			}
			if flags.JSON {
				return printJSON(cmd, out)
			}
			cmd.Printf("profile %s updated\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "модель (пусто — модель агента по умолчанию)")
	cmd.Flags().StringVar(&effort, "effort", "", "уровень усилия (пусто — по умолчанию)")
	cmd.Flags().StringVar(&description, "description", "", "для чего подходит профиль")
	cmd.Flags().BoolVar(&enable, "enable", false, "включить профиль")
	cmd.Flags().BoolVar(&disable, "disable", false, "выключить профиль (запуски с ним запрещены)")
	cmd.Flags().IntVar(&position, "position", 0, "порядок в списках")
	return cmd
}

func newModelsRmCmd(dial func() (modelsClient, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Удалить профиль (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket models rm <name>"}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			if err := c.Delete(apiPath("v1", "model-profiles", args[0]), nil, nil); err != nil {
				return explainHumanOnly(err)
			}
			cmd.Printf("profile %s removed\n", args[0])
			return nil
		},
	}
}

func newModelsDefaultCmd(dial func() (modelsClient, error)) *cobra.Command {
	var orch, worker string
	cmd := &cobra.Command{
		Use:   "default",
		Short: "Профили по умолчанию для оркестратора и воркеров (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if cmd.Flags().Changed("orchestrator") {
				body["default_orchestrator_profile"] = orch
			}
			if cmd.Flags().Changed("worker") {
				body["default_worker_profile"] = worker
			}
			if len(args) != 0 || len(body) == 0 {
				return &usageError{message: "usage: rocket models default --orchestrator <name> | --worker <name>"}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			if err := c.Put("/v1/settings", body, nil); err != nil {
				return explainHumanOnly(err)
			}
			cmd.Println("defaults updated")
			return nil
		},
	}
	cmd.Flags().StringVar(&orch, "orchestrator", "", "профиль оркестратора по умолчанию")
	cmd.Flags().StringVar(&worker, "worker", "", "профиль воркера по умолчанию")
	return cmd
}

// explainHumanOnly turns the daemon's 403 human_only into a sentence an
// agent cannot misread as a transient failure to retry or work around.
func explainHumanOnly(err error) error {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "human_only" {
		return fmt.Errorf("human_only: only the human manages model profiles, the defaults and task allowlists "+
			"(rocket models / rocket task models from their own terminal, or the dashboard); "+
			"an agent session may not — ask the human instead (%s)", apiErr.Message)
	}
	return err
}

// parseProfileList splits a --allow value "a, b,c" into names.
func parseProfileList(s string) []string {
	out := []string{}
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func newTaskModelsCmd() *cobra.Command {
	return newTaskModelsCmdWith(connectModels)
}

// newTaskModelsCmdWith builds `rocket task models` with dial as its daemon
// connection (tests pass a fake).
func newTaskModelsCmdWith(dial func() (modelsClient, error)) *cobra.Command {
	var allow string
	var clear bool
	cmd := &cobra.Command{
		Use:   "models <id>",
		Short: "Профиль оркестратора и разрешённые воркерам профили задачи; --allow/--clear меняют список (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			const usage = "usage: rocket task models <id> [--allow p1,p2 | --clear]"
			if len(args) != 1 || (clear && cmd.Flags().Changed("allow")) {
				return &usageError{message: usage}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "invalid task id"}
			}
			c, err := dial()
			if err != nil {
				return err
			}
			path := apiPath("v1", "tasks", args[0])
			var task struct {
				AllowedProfiles     []string `json:"allowed_profiles"`
				OrchestratorProfile string   `json:"orchestrator_profile"`
			}
			switch {
			case clear:
				err = c.Patch(path, map[string]any{"allowed_profiles": []string{}}, &task)
			case cmd.Flags().Changed("allow"):
				err = c.Patch(path, map[string]any{"allowed_profiles": parseProfileList(allow)}, &task)
			default:
				err = c.Get(path, nil, &task)
			}
			if err != nil {
				return explainHumanOnly(err)
			}
			if flags.JSON {
				return printJSON(cmd, task)
			}
			cmd.Printf("orchestrator: %s\n", dash(task.OrchestratorProfile))
			if len(task.AllowedProfiles) == 0 {
				cmd.Println("allowed: all enabled profiles")
			} else {
				cmd.Printf("allowed: %s\n", strings.Join(task.AllowedProfiles, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&allow, "allow", "", "разрешить воркерам только эти профили (через запятую)")
	cmd.Flags().BoolVar(&clear, "clear", false, "снять ограничение: все включённые профили")
	return cmd
}
