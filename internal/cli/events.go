package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/spf13/cobra"
)

// eventRow is the subset of the API's event JSON shape needed to render
// `rocket events`. Field names/tags mirror internal/api.eventResponse.
type eventRow struct {
	ID        int64          `json:"id"`
	TS        int64          `json:"ts"`
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// followPollInterval is how often `rocket events --follow` polls for new
// events. SSE streaming is deferred to phase 2.
const followPollInterval = 2 * time.Second

func newEventsCmd() *cobra.Command {
	var follow bool
	var session string

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Показать журнал событий",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket events [--follow] [--session <id>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			events, err := fetchEventsTail(c, session, 50)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			printEvents(w, events, flags.JSON)

			if !follow {
				return nil
			}

			var lastID int64
			if len(events) > 0 {
				lastID = events[len(events)-1].ID
			}

			return followEvents(cmd.Context(), c, w, session, lastID)
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "продолжать выводить новые события")
	cmd.Flags().StringVar(&session, "session", "", "фильтр по id сессии")
	return cmd
}

// fetchEventsTail fetches the last limit events (optionally filtered by
// session) from the daemon, ascending by id.
func fetchEventsTail(c *client.Client, session string, limit int) ([]eventRow, error) {
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if session != "" {
		q.Set("session", session)
	}

	var resp struct {
		Events []eventRow `json:"events"`
	}
	if err := c.Get("/v1/events?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// fetchEventsSince fetches events with id > since (optionally filtered by
// session), ascending by id.
func fetchEventsSince(c *client.Client, session string, since int64) ([]eventRow, error) {
	q := url.Values{}
	q.Set("since", fmt.Sprintf("%d", since))
	if session != "" {
		q.Set("session", session)
	}

	var resp struct {
		Events []eventRow `json:"events"`
	}
	if err := c.Get("/v1/events?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

// followEvents polls the daemon for new events (id > lastID) every
// followPollInterval until interrupted by SIGINT/SIGTERM or ctx is
// cancelled, at which point it returns nil (a clean exit).
func followEvents(ctx context.Context, c *client.Client, w io.Writer, session string, lastID int64) error {
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCtx.Done():
			return nil
		case <-ticker.C:
			events, err := fetchEventsSince(c, session, lastID)
			if err != nil {
				return err
			}
			if len(events) == 0 {
				continue
			}
			printEvents(w, events, flags.JSON)
			lastID = events[len(events)-1].ID
		}
	}
}

// printEvents renders events either as JSON lines or as
// "<id> <RFC3339 ts> <type> <session_id> <compact json data>".
func printEvents(w io.Writer, events []eventRow, asJSON bool) {
	for _, e := range events {
		if asJSON {
			enc := json.NewEncoder(w)
			_ = enc.Encode(e)
			continue
		}
		dataJSON, _ := json.Marshal(e.Data)
		ts := time.Unix(e.TS, 0).UTC().Format(time.RFC3339)
		fmt.Fprintf(w, "%d %s %s %s %s\n", e.ID, ts, e.Type, e.SessionID, string(dataJSON))
	}
}
