package cli

import (
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <feature-slug>",
		Short: "Показать статус фичи: оркестратор и его воркеры",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket status <feature-slug>"}
			}
			slug := args[0]

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			q := url.Values{}
			q.Set("feature", slug)
			path := "/v1/sessions?" + q.Encode()

			var sessions []sessionRow
			if err := c.Get(path, nil, &sessions); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, sessions)
			}

			if len(sessions) == 0 {
				cmd.Printf("no live sessions for feature %s\n", slug)
				return nil
			}

			renderStatus(slug, sessions, cmd.OutOrStdout(), time.Now())
			return nil
		},
	}
	return cmd
}

// renderStatus writes a feature status view to w: a header line naming the
// slug, then the orchestrator's own line ("orchestrator: <id> [state]
// <activity> (<age> ago)", or "orchestrator: -" if none is live), followed
// by a worker table (SESSION, ACTIVITY, PR, CI, AGE) when any workers are present.
func renderStatus(slug string, sessions []sessionRow, w io.Writer, now time.Time) {
	var orch *sessionRow
	var workers []sessionRow
	for i := range sessions {
		s := sessions[i]
		if s.Kind == "orchestrator" {
			o := s
			orch = &o
		} else {
			workers = append(workers, s)
		}
	}

	fmt.Fprintf(w, "feature: %s\n", slug)
	if orch != nil {
		activity := orch.Activity
		if activity == "" {
			activity = "-"
		}
		fmt.Fprintf(w, "orchestrator: %s [%s] %s (%s ago)\n", orch.ID, orch.State, activity, humanAge(orch.CreatedAt, now))
	} else {
		fmt.Fprintf(w, "orchestrator: -\n")
	}

	if len(workers) > 0 {
		fmt.Fprintf(w, "\n")
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = tw.Write([]byte("SESSION\tACTIVITY\tPR\tCI\tAGE\n"))
		for _, wk := range workers {
			activity := wk.Activity
			if activity == "" {
				activity = "-"
			}
			pr := "-"
			if wk.PRNumber > 0 {
				pr = fmt.Sprintf("#%d", wk.PRNumber)
			}
			ci := wk.CIState
			if ci == "" {
				ci = "-"
			}
			_, _ = tw.Write([]byte(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\n", wk.ID, activity, pr, ci, humanAge(wk.CreatedAt, now))))
		}
		_ = tw.Flush()
	}
}
