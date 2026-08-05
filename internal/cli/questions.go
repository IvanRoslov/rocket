// The unified thread inbox: "rocket questions" (task #1023, spec v1 §«Единый
// инбокс»). It answers "what is open and on whom" for task and role threads in
// one screen — the question that previously required walking every task and
// every role, which is exactly how threads got missed and left hanging.
package cli

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// threadRow is one line of the inbox, mirroring internal/api.threadInboxEntry.
// It carries the question body only — a thread's conversation is read with
// "rocket task questions" / "rocket agent questions".
type threadRow struct {
	LocalRef     string   `json:"local_ref"`
	Kind         string   `json:"kind"`
	TaskID       int64    `json:"task_id,omitempty"`
	RoleID       string   `json:"role_id,omitempty"`
	Subject      string   `json:"subject"`
	ID           int64    `json:"id"`
	Ordinal      int      `json:"ordinal"`
	AskedBy      string   `json:"asked_by"`
	Body         string   `json:"body"`
	Status       string   `json:"status"`
	Resolution   string   `json:"resolution,omitempty"`
	Type         string   `json:"type"`
	Options      []string `json:"options,omitempty"`
	Participants []string `json:"participants"`
	Attention    []string `json:"attention"`
	WaitingOn    []string `json:"waiting_on"`
	YourTurn     bool     `json:"your_turn"`
	AskedAt      int64    `json:"asked_at"`
	UpdatedAt    int64    `json:"updated_at"`
	ResolvedAt   int64    `json:"resolved_at,omitempty"`
}

// threadInboxPath builds the request path. Both filters are applied by the
// daemon: an inbox that fetched everything and sieved it locally would show a
// caller threads they may not read.
func threadInboxPath(waitingOn string, all bool) string {
	q := url.Values{}
	if all {
		q.Set("all", "true")
	}
	if waitingOn != "" {
		q.Set("waiting_on", waitingOn)
	}
	if len(q) == 0 {
		return "/v1/threads"
	}
	return "/v1/threads?" + q.Encode()
}

// renderThreadInbox renders the listing: per thread a header line
// "<local-ref> [<state>] <subject> (<age>)", the first line of the question,
// its options, its participants, and who is awaited.
func renderThreadInbox(threads []threadRow, now time.Time) string {
	if len(threads) == 0 {
		return "нет открытых тредов\n"
	}

	var sb strings.Builder
	for _, th := range threads {
		fmt.Fprintf(&sb, "%s [%s] %s (%s)\n",
			th.LocalRef, threadStatusLabel(th.Status, th.Type), th.Subject,
			humanAge(th.UpdatedAt, now))
		fmt.Fprintf(&sb, "  %s\n", firstLine(th.Body))
		renderThreadOptions(&sb, th.Options)
		renderParticipantsLine(&sb, th.Participants)
		if arrow := threadTurnArrow(th.Attention, th.YourTurn); arrow != "" {
			// threadTurnArrow is written as a header suffix (" → ждут: …"); on
			// its own line it takes the same indent as the lines above it.
			fmt.Fprintf(&sb, "  %s\n", strings.TrimSpace(arrow))
		}
	}
	return sb.String()
}

func newQuestionsCmd() *cobra.Command {
	var waitingOn string
	var all bool

	const usage = "usage: rocket questions [--waiting-on <id>] [--all]"

	cmd := &cobra.Command{
		Use:   "questions",
		Short: "Все открытые треды — задач и ролей — одним списком",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return &usageError{message: usage}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp struct {
				Threads []threadRow `json:"threads"`
			}
			if err := c.Get(threadInboxPath(waitingOn, all), nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp.Threads)
			}
			cmd.Print(renderThreadInbox(resp.Threads, time.Now()))
			return nil
		},
	}
	cmd.Flags().StringVar(&waitingOn, "waiting-on", "",
		"показать только треды, которые ждут этого участника (human, id агента или сессии)")
	cmd.Flags().BoolVar(&all, "all", false, "включить закрытые треды (в том числе fyi)")
	return cmd
}
