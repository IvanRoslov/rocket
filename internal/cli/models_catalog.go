package cli

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// catalogReplyRow is one agent of GET /v1/model-catalog.
type catalogReplyRow struct {
	Agent     string     `json:"agent"`
	Source    string     `json:"source"`
	FetchedAt *time.Time `json:"fetched_at"`
	Warning   string     `json:"warning"`
	Models    []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Main          bool     `json:"main"`
		Efforts       []string `json:"efforts"`
		DefaultEffort string   `json:"default_effort"`
	} `json:"models"`
}

type catalogReplyBody struct {
	Agents []catalogReplyRow `json:"agents"`
}

func newModelsCatalogCmd(dial func() (modelsClient, error)) *cobra.Command {
	var agentName string
	var all, refresh bool
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Модели, которые предлагают агенты (из их CLI, кэша или встроенного списка)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket models catalog [--agent A] [--all] [--refresh]"}
			}
			q := url.Values{}
			if agentName != "" {
				q.Set("agent", agentName)
			}
			if refresh {
				q.Set("refresh", "1")
			}
			path := "/v1/model-catalog"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var reply catalogReplyBody
			if err := c.Get(path, nil, &reply); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, reply)
			}
			return printCatalog(cmd.OutOrStdout(), reply, all)
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "только этот агент: claude-code | codex")
	cmd.Flags().BoolVar(&all, "all", false, "с предыдущими моделями")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "перечитать источники, минуя кэш демона (10 мин)")
	return cmd
}

// printCatalog prints the models as a table (the default effort marked
// with *), then one source line per agent with its warning, if any.
func printCatalog(w io.Writer, reply catalogReplyBody, all bool) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if all {
		fmt.Fprintln(tw, "AGENT\tID\tNAME\tPREVIOUS\tEFFORTS\tDESCRIPTION")
	} else {
		fmt.Fprintln(tw, "AGENT\tID\tNAME\tEFFORTS\tDESCRIPTION")
	}
	for _, a := range reply.Agents {
		for _, m := range a.Models {
			if !m.Main && !all {
				continue
			}
			efforts := make([]string, 0, len(m.Efforts))
			for _, e := range m.Efforts {
				if e == m.DefaultEffort {
					e += "*"
				}
				efforts = append(efforts, e)
			}
			eff := dash(strings.Join(efforts, ","))
			if all {
				prev := "-"
				if !m.Main {
					prev = "yes"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Agent, m.ID, m.Name, prev, eff, m.Description)
			} else {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.Agent, m.ID, m.Name, eff, m.Description)
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w, "\n* = усилие модели по умолчанию")
	for _, a := range reply.Agents {
		line := a.Agent + ": " + catalogSourceLabel(a.Agent, a.Source)
		if a.FetchedAt != nil {
			line += " (" + a.FetchedAt.UTC().Format("2006-01-02 15:04 UTC") + ")"
		}
		if a.Warning != "" {
			line += " — предупреждение: " + a.Warning
		}
		fmt.Fprintln(w, line)
	}
	return nil
}

// catalogSourceLabel names where an agent's catalog came from, as the
// dashboard does.
func catalogSourceLabel(agentName, source string) string {
	switch source {
	case "cli":
		if agentName == "codex" {
			return "из Codex CLI"
		}
		return "из CLI агента"
	case "cache":
		switch agentName {
		case "claude-code":
			return "из кэша Claude Code"
		case "codex":
			return "из кэша Codex"
		}
		return "из кэша агента"
	case "builtin":
		return "встроенный список"
	}
	return source
}

// importReply is POST /v1/model-profiles/import-catalog.
type importReply struct {
	Created []string `json:"created"`
	Skipped []struct {
		Model  string `json:"model"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func newModelsImportCmd(dial func() (modelsClient, error)) *cobra.Command {
	var agentName string
	var legacy bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Создать выключенные профили для моделей каталога без профиля (только человек)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket models import [--agent A] [--legacy]"}
			}
			body := map[string]any{"include_legacy": legacy}
			if agentName != "" {
				body["agent"] = agentName
			}
			c, err := dial()
			if err != nil {
				return err
			}
			var reply importReply
			if err := c.Post("/v1/model-profiles/import-catalog", body, &reply); err != nil {
				return explainHumanOnly(err)
			}
			if flags.JSON {
				return printJSON(cmd, reply)
			}
			w := cmd.OutOrStdout()
			if len(reply.Created) == 0 {
				fmt.Fprintln(w, "ничего не создано: у всех моделей каталога уже есть профили")
			} else {
				fmt.Fprintf(w, "создано профилей: %d (выключены — включите нужные: rocket models edit <name> --enable)\n", len(reply.Created))
				for _, n := range reply.Created {
					fmt.Fprintln(w, "  "+n)
				}
			}
			if len(reply.Skipped) > 0 {
				fmt.Fprintln(w, "пропущено:")
				for _, s := range reply.Skipped {
					fmt.Fprintf(w, "  %s — %s\n", s.Model, s.Reason)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "только этот агент: claude-code | codex")
	cmd.Flags().BoolVar(&legacy, "legacy", false, "включая предыдущие модели")
	return cmd
}
