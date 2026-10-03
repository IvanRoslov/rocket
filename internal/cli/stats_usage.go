package cli

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/IvanRoslov/rocket/internal/usage"
	"github.com/spf13/cobra"
)

type statsClient interface {
	Get(path string, in, out any) error
	Put(path string, in, out any) error
	Delete(path string, in, out any) error
}

func dialStatsClient() (statsClient, error) {
	c, _, err := connect(true)
	if err != nil {
		return nil, err
	}
	return c, nil
}

type statsUsageReply struct {
	From string `json:"from"`
	To   string `json:"to"`
	usage.PeriodSummary
	Pending int `json:"pending"`
}

type statsPriceRow struct {
	Model      string   `json:"model"`
	Input      *float64 `json:"input"`
	CacheWrite *float64 `json:"cache_write"`
	CacheRead  *float64 `json:"cache_read"`
	Output     *float64 `json:"output"`
	UpdatedAt  *int64   `json:"updated_at"`
}

type statsPricesReply struct {
	Prices []statsPriceRow `json:"prices"`
}

type statsPriceInput struct {
	Input      *float64 `json:"input"`
	CacheWrite *float64 `json:"cache_write"`
	CacheRead  *float64 `json:"cache_read"`
	Output     *float64 `json:"output"`
}

func newStatsUsageCmd(dial func() (statsClient, error)) *cobra.Command {
	var from, to, project string
	cmd := &cobra.Command{
		Use: "usage", Short: "Расход токенов и стоимость по моделям и задачам",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket stats usage [--from YYYY-MM-DD] [--to YYYY-MM-DD] [--project ID]"}
			}
			query := url.Values{}
			if from != "" {
				query.Set("from", from)
			}
			if to != "" {
				query.Set("to", to)
			}
			if project != "" {
				query.Set("project", project)
			}
			path := apiPath("v1", "stats", "usage")
			if len(query) > 0 {
				path += "?" + query.Encode()
			}
			client, err := dial()
			if err != nil {
				return err
			}
			var reply statsUsageReply
			if err := client.Get(path, nil, &reply); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, reply)
			}
			cmd.Print(renderUsageStats(reply))
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "начало периода (локальная дата демона)")
	cmd.Flags().StringVar(&to, "to", "", "конец периода включительно")
	cmd.Flags().StringVar(&project, "project", "", "только один проект")
	return cmd
}

func newStatsTaskCmd(dial func() (statsClient, error)) *cobra.Command {
	return &cobra.Command{
		Use: "task <id>", Short: "Расход сессий корневой задачи",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket stats task <id>"}
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return &usageError{message: "task id must be a positive integer"}
			}
			client, err := dial()
			if err != nil {
				return err
			}
			var reply usage.TaskUsageSummary
			if err := client.Get(apiPath("v1", "tasks", strconv.FormatInt(id, 10), "usage"), nil, &reply); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, reply)
			}
			cmd.Print(renderTaskUsage(reply))
			return nil
		},
	}
}

func newStatsPricesCmd(dial func() (statsClient, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use: "prices", Short: "Прайс моделей ($ за 1 млн токенов)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket stats prices [set <model> | rm <model>]"}
			}
			client, err := dial()
			if err != nil {
				return err
			}
			var reply statsPricesReply
			if err := client.Get(apiPath("v1", "stats", "prices"), nil, &reply); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, reply)
			}
			cmd.Print(renderPrices(reply.Prices))
			return nil
		},
	}
	cmd.AddCommand(newStatsPricesSetCmd(dial))
	cmd.AddCommand(newStatsPricesRmCmd(dial))
	return cmd
}

func newStatsPricesSetCmd(dial func() (statsClient, error)) *cobra.Command {
	var input, cacheWrite, cacheRead, output string
	cmd := &cobra.Command{
		Use: "set <model>", Short: "Задать цены модели; незаданные поля сохраняются, null очищает",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return &usageError{message: "usage: rocket stats prices set <model> [--input X] [--cache-write X] [--cache-read X] [--output X]"}
			}
			values := []struct {
				name, raw string
				dest      **float64
			}{}
			var changed bool
			for _, name := range []string{"input", "cache-write", "cache-read", "output"} {
				if cmd.Flags().Changed(name) {
					changed = true
				}
			}
			if !changed {
				return &usageError{message: "at least one price flag is required"}
			}
			var body statsPriceInput
			values = append(values,
				struct {
					name, raw string
					dest      **float64
				}{"input", input, &body.Input},
				struct {
					name, raw string
					dest      **float64
				}{"cache-write", cacheWrite, &body.CacheWrite},
				struct {
					name, raw string
					dest      **float64
				}{"cache-read", cacheRead, &body.CacheRead},
				struct {
					name, raw string
					dest      **float64
				}{"output", output, &body.Output})
			for _, v := range values {
				if !cmd.Flags().Changed(v.name) {
					continue
				}
				parsed, err := parseStatsRate(v.raw)
				if err != nil {
					return &usageError{message: "--" + v.name + ": " + err.Error()}
				}
				*v.dest = parsed
			}
			client, err := dial()
			if err != nil {
				return err
			}
			var current statsPricesReply
			if err := client.Get(apiPath("v1", "stats", "prices"), nil, &current); err != nil {
				return err
			}
			for _, p := range current.Prices {
				if p.Model != args[0] {
					continue
				}
				if !cmd.Flags().Changed("input") {
					body.Input = p.Input
				}
				if !cmd.Flags().Changed("cache-write") {
					body.CacheWrite = p.CacheWrite
				}
				if !cmd.Flags().Changed("cache-read") {
					body.CacheRead = p.CacheRead
				}
				if !cmd.Flags().Changed("output") {
					body.Output = p.Output
				}
				break
			}
			var saved statsPriceRow
			if err := client.Put(apiPath("v1", "stats", "prices", args[0]), body, &saved); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, saved)
			}
			cmd.Printf("Прайс %s сохранён\n", saved.Model)
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "$/1M входных токенов, либо null")
	cmd.Flags().StringVar(&cacheWrite, "cache-write", "", "$/1M записи кэша, либо null")
	cmd.Flags().StringVar(&cacheRead, "cache-read", "", "$/1M чтения кэша, либо null")
	cmd.Flags().StringVar(&output, "output", "", "$/1M выходных токенов, либо null")
	return cmd
}

func parseStatsRate(raw string) (*float64, error) {
	if raw == "null" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || v < 0 {
		return nil, fmt.Errorf("expected a nonnegative number or null")
	}
	return &v, nil
}

func newStatsPricesRmCmd(dial func() (statsClient, error)) *cobra.Command {
	return &cobra.Command{
		Use: "rm <model>", Short: "Удалить прайс модели",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
				return &usageError{message: "usage: rocket stats prices rm <model>"}
			}
			client, err := dial()
			if err != nil {
				return err
			}
			if err := client.Delete(apiPath("v1", "stats", "prices", args[0]), nil, nil); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, map[string]any{"model": args[0], "deleted": true})
			}
			cmd.Printf("Прайс %s удалён\n", args[0])
			return nil
		},
	}
}

func costText(cost *float64, partial bool) string {
	if cost == nil {
		return "—"
	}
	label := fmt.Sprintf("$%.4f", *cost)
	if partial {
		label += " (partial)"
	}
	return label
}

func renderUsageStats(s statsUsageReply) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Расход %s — %s\n", s.From, s.To)
	fmt.Fprintf(&b, "Сессий: %d  Токенов: %d  Чтение кэша: %d  Стоимость: %s\n",
		s.Totals.Sessions, s.Totals.Tokens.Billable, s.Totals.Tokens.CacheRead, costText(s.Totals.CostUSD, s.Totals.CostPartial))
	if s.Pending > 0 {
		fmt.Fprintf(&b, "Идёт подсчёт истории: %d сессий\n", s.Pending)
	}
	if len(s.Models) == 0 && len(s.Tasks) == 0 {
		b.WriteString("нет расхода за период\n")
		return b.String()
	}
	b.WriteString("\nПо моделям\n")
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "МОДЕЛЬ\tАГЕНТ\tСЕССИЙ\tТОКЕНЫ\tINPUT\tCACHE WRITE\tCACHE READ\tOUTPUT\t≈ $")
	for _, m := range s.Models {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			m.Model, m.Agent, m.Sessions, m.Tokens.Billable, m.Tokens.Input, m.Tokens.CacheWrite, m.Tokens.CacheRead, m.Tokens.Output, costText(m.CostUSD, false))
	}
	_ = tw.Flush()
	b.WriteString("\nПо задачам\n")
	tw = tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ЗАДАЧА\tПРОЕКТ\tСТАТУС\tСЕССИЙ\tТОКЕНЫ\t≈ $")
	for _, task := range s.Tasks {
		name := "Без задачи"
		if task.TaskID != nil {
			name = fmt.Sprintf("#%d %s", *task.TaskID, task.Title)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n", name, task.ProjectID, task.Status,
			task.Sessions, task.Tokens.Billable, costText(task.CostUSD, task.CostPartial))
	}
	_ = tw.Flush()
	return b.String()
}

func renderTaskUsage(s usage.TaskUsageSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Задача #%d: %d сессий, %d токенов, стоимость %s\n", s.TaskID,
		s.Totals.Sessions, s.Totals.Tokens.Billable, costText(s.Totals.CostUSD, s.Totals.CostPartial))
	if len(s.Sessions) == 0 {
		b.WriteString("нет сессий\n")
		return b.String()
	}
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "СЕССИЯ\tРОЛЬ\tПОДЗАДАЧА\tPR\tМОДЕЛИ\tДЛИТЕЛЬНОСТЬ\tТОКЕНЫ\tCACHE READ\t≈ $\tСТАТУС")
	for _, session := range s.Sessions {
		models := make([]string, 0, len(session.Models))
		for _, model := range session.Models {
			models = append(models, model.Model)
		}
		duration := "—"
		if session.DurationS != nil {
			duration = fmt.Sprintf("%ds", *session.DurationS)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			session.SessionID, session.Role, session.SubtaskTitle, session.PRURL, strings.Join(models, ", "),
			duration, session.Tokens.Billable, session.Tokens.CacheRead, costText(session.CostUSD, session.CostPartial), session.Status)
	}
	_ = tw.Flush()
	return b.String()
}

func rateText(rate *float64) string {
	if rate == nil {
		return "—"
	}
	return strconv.FormatFloat(*rate, 'f', -1, 64)
}

func renderPrices(prices []statsPriceRow) string {
	var b strings.Builder
	if len(prices) == 0 {
		return "нет моделей\n"
	}
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "МОДЕЛЬ\tINPUT\tCACHE WRITE\tCACHE READ\tOUTPUT")
	for _, p := range prices {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Model, rateText(p.Input),
			rateText(p.CacheWrite), rateText(p.CacheRead), rateText(p.Output))
	}
	_ = tw.Flush()
	return b.String()
}
