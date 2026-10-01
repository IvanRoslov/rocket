package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// maxStatsWeeks mirrors the daemon's upper bound on ?weeks.
const maxStatsWeeks = 520

// brainstormWeekRow mirrors internal/api.brainstormWeekResponse.
type brainstormWeekRow struct {
	Week                string `json:"week"`
	Skill               string `json:"skill"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
}

// brainstormStormRow mirrors internal/api.brainstormStormResponse.
type brainstormStormRow struct {
	TaskID              int64  `json:"task_id"`
	Title               string `json:"title"`
	ProjectID           string `json:"project_id"`
	Skill               string `json:"skill"`
	Questions           int    `json:"questions"`
	Answered            int    `json:"answered"`
	Accepted            int    `json:"accepted"`
	AcceptedWithComment int    `json:"accepted_with_comment"`
	Corrected           int    `json:"corrected"`
	WrongTurn           int    `json:"wrong_turn"`
	SpecChanges         int    `json:"spec_changes"`
	GoAt                *int64 `json:"go_at"`
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

// renderBrainstormStats renders the weekly summary and the storms table.
func renderBrainstormStats(s brainstormStats, weeks int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Брейншторм по неделям (последние %d нед.)\n", weeks)
	if len(s.Weeks) == 0 {
		b.WriteString("  ответов на вопросы шторма нет\n")
	} else {
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "НЕДЕЛЯ\tСКИЛЛ\tОТВЕЧЕНО\tПРИНЯТО\tС КОММЕНТАРИЕМ\tПОПРАВЛЕНО\tНЕ ТУДА")
		for _, w := range s.Weeks {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%d\t%d\n", w.Week, w.Skill, w.Answered,
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
	fmt.Fprintln(tw, "ЗАДАЧА\tСКИЛЛ\tВОПРОСОВ\tОТВЕЧЕНО\tПРИНЯТО\tС КОММЕНТАРИЕМ\tПОПРАВЛЕНО\tНЕ ТУДА\tПРАВОК СПЕКИ\tGO\tНАЗВАНИЕ")
	for _, st := range s.Storms {
		goAt := "—"
		if st.GoAt != nil {
			goAt = time.Unix(*st.GoAt, 0).Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "#%d\t%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n", st.TaskID, st.Skill, st.Questions,
			st.Answered, acceptedShare(st.Accepted, st.Answered), st.AcceptedWithComment, st.Corrected, st.WrongTurn,
			st.SpecChanges, goAt, st.Title)
	}
	_ = tw.Flush()
	return b.String()
}
