package cli

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

// maxStatsWeeks mirrors the daemon's upper bound on ?weeks.
const maxStatsWeeks = 520

// brainstormWeekRow mirrors internal/api.brainstormWeekResponse.
type brainstormWeekRow struct {
	Week                string `json:"week"`
	Skill               string `json:"skill"`
	AnsweredBy          string `json:"answered_by"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
}

// brainstormAnswererRow mirrors internal/api.brainstormAnswererResponse.
type brainstormAnswererRow struct {
	AnsweredBy          string `json:"answered_by"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
}

// brainstormStormRow mirrors internal/api.brainstormStormResponse.
type brainstormStormRow struct {
	TaskID              int64                   `json:"task_id"`
	Title               string                  `json:"title"`
	ProjectID           string                  `json:"project_id"`
	Skill               string                  `json:"skill"`
	Questions           int                     `json:"questions"`
	Answered            int                     `json:"answered"`
	Accepted            int                     `json:"accepted"`
	AcceptedWithComment int                     `json:"accepted_with_comment"`
	Corrected           int                     `json:"corrected"`
	WrongTurn           int                     `json:"wrong_turn"`
	AnsweredBy          []string                `json:"answered_by"`
	ByAnswerer          []brainstormAnswererRow `json:"by_answerer"`
	SpecChanges         int                     `json:"spec_changes"`
	FirstTryGo          bool                    `json:"first_try_go"`
	HasGate             bool                    `json:"has_gate"`
	GoAt                *int64                  `json:"go_at"`
}

// brainstormStats mirrors internal/api.brainstormStatsResponse.
type brainstormStats struct {
	Weeks  []brainstormWeekRow  `json:"weeks"`
	Storms []brainstormStormRow `json:"storms"`
}

// newStatsCmd builds "rocket stats": read-only metrics of the daemon.
func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Метрики",
	}
	cmd.AddCommand(newStatsBrainstormCmd())
	cmd.AddCommand(newStatsCollectCmd())
	cmd.AddCommand(newStatsUsageCmd(dialStatsClient))
	cmd.AddCommand(newStatsTaskCmd(dialStatsClient))
	cmd.AddCommand(newStatsPricesCmd(dialStatsClient))
	return cmd
}

func newStatsBrainstormCmd() *cobra.Command {
	var weeks int
	cmd := &cobra.Command{
		Use:   "brainstorm",
		Short: "Метрика брейншторма: доля принятых рекомендаций по неделям и штормы",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket stats brainstorm [--weeks N] [--json]"}
			}
			if weeks < 1 || weeks > maxStatsWeeks {
				return &usageError{message: fmt.Sprintf("--weeks must be from 1 to %d", maxStatsWeeks)}
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var resp brainstormStats
			if err := c.Get(apiPath("v1", "stats", "brainstorm")+"?weeks="+strconv.Itoa(weeks), nil, &resp); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Print(renderBrainstormStats(resp, weeks))
			return nil
		},
	}
	cmd.Flags().IntVar(&weeks, "weeks", 12, "сколько последних ISO-недель показать")
	return cmd
}

// acceptedShare renders accepted with its share of answered, "6 (60%)";
// "—" for the share when nothing was answered.
func acceptedShare(accepted, answered int) string {
	if answered == 0 {
		return fmt.Sprintf("%d (—)", accepted)
	}
	pct := math.Round(float64(accepted) * 100 / float64(answered))
	return fmt.Sprintf("%d (%.0f%%)", accepted, pct)
}

// participantLabel names a metric participant: the human is «Иван», an
// agent its id verbatim.
func participantLabel(id string) string {
	if id == "" || id == "human" {
		return "Иван"
	}
	return id
}

// stormWho names who stormed: participants in first-answer order joined with
// " + ", "—" when nobody answered.
func stormWho(ids []string) string {
	if len(ids) == 0 {
		return "—"
	}
	labels := make([]string, len(ids))
	for i, id := range ids {
		labels[i] = participantLabel(id)
	}
	return strings.Join(labels, " + ")
}

// gateState is how the human received the storm's spec gate.
func gateState(goAt *int64, specChanges int, hasGate bool) string {
	switch {
	case goAt != nil && specChanges == 0:
		return "Go с 1-го раза"
	case goAt != nil:
		return fmt.Sprintf("Go после %d правок", specChanges)
	case hasGate:
		return fmt.Sprintf("ждёт Go (правок: %d)", specChanges)
	default:
		return "—"
	}
}

// renderBrainstormStats renders the weekly summary and the storms table.
func renderBrainstormStats(s brainstormStats, weeks int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Брейншторм по неделям (последние %d нед.)\n", weeks)
	if len(s.Weeks) == 0 {
		b.WriteString("  ответов на вопросы шторма нет\n")
	} else {
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "НЕДЕЛЯ\tСКИЛЛ\tКТО\tОТВЕЧЕНО\tПРИНЯТО\tС КОММЕНТАРИЕМ\tПОПРАВЛЕНО\tНЕ ТУДА")
		for _, w := range s.Weeks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%d\t%d\t%d\n", w.Week, w.Skill, participantLabel(w.AnsweredBy), w.Answered,
				acceptedShare(w.Accepted, w.Answered), w.AcceptedWithComment, w.Corrected, w.WrongTurn)
		}
		_ = tw.Flush()
	}

	b.WriteString("\nШтормы\n")
	if len(s.Storms) == 0 {
		b.WriteString("  штормов нет\n")
		return b.String()
	}
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ЗАДАЧА\tСКИЛЛ\tКТО ШТОРМИЛ\tВОПРОСОВ\tОТВЕЧЕНО\tПРИНЯТО\tС КОММЕНТАРИЕМ\tПОПРАВЛЕНО\tНЕ ТУДА\tПРАВОК ДО GO\tГЕЙТ\tGO\tНАЗВАНИЕ")
	for _, st := range s.Storms {
		goAt := "—"
		if st.GoAt != nil {
			goAt = time.Unix(*st.GoAt, 0).Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%s\t%s\t%s\n", st.TaskID, st.Skill,
			stormWho(st.AnsweredBy), st.Questions, st.Answered, acceptedShare(st.Accepted, st.Answered),
			st.AcceptedWithComment, st.Corrected, st.WrongTurn, st.SpecChanges,
			gateState(st.GoAt, st.SpecChanges, st.HasGate), goAt, st.Title)
	}
	_ = tw.Flush()
	return b.String()
}

const statsCollectUsage = "usage: rocket stats collect [--session S | --all] [--retry-missing]"

// newStatsCollectCmd builds "rocket stats collect": queue a re-collection of
// token usage from agent transcripts (human-only on the daemon side).
func newStatsCollectCmd() *cobra.Command {
	var session string
	var all, retryMissing bool
	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Пересчитать расход токенов сессий из транскриптов",
		RunE: func(cmd *cobra.Command, args []string) error {
			bulk := all || retryMissing
			if len(args) != 0 || (session == "") == !bulk {
				return &usageError{message: statsCollectUsage}
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			return runStatsCollect(c, cmd.OutOrStdout(), session, all, retryMissing)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "пересчитать одну сессию (живую — снимком)")
	cmd.Flags().BoolVar(&all, "all", false, "пересчитать все завершённые сессии, кроме missing")
	cmd.Flags().BoolVar(&retryMissing, "retry-missing", false, "заново искать транскрипты у сессий со статусом missing")
	return cmd
}

func runStatsCollect(c *client.Client, out io.Writer, session string, all, retryMissing bool) error {
	body := map[string]any{}
	if session != "" {
		body["session_id"] = session
	}
	if all {
		body["all"] = true
	}
	if retryMissing {
		body["retry_missing"] = true
	}
	var resp struct {
		Queued int `json:"queued"`
	}
	if err := c.Post(apiPath("v1", "stats", "usage", "collect"), body, &resp); err != nil {
		return err
	}
	if session != "" {
		fmt.Fprintf(out, "сессия %s поставлена в очередь сбора\n", session)
		return nil
	}
	fmt.Fprintf(out, "в очереди сбора: %d сессий\n", resp.Queued)
	return nil
}
