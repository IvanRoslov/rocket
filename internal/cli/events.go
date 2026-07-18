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

// sseEventPayload mirrors the JSON payload sent as the "data:" field of
// each SSE frame from GET /v1/events/stream (internal/api.eventResponse).
type sseEventPayload struct {
	ID        int64          `json:"id"`
	TS        int64          `json:"ts"`
	Type      string         `json:"type"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

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
// events when it has fallen back to polling (SSE streaming unavailable).
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

// followEvents streams new events (id > lastID) to w until interrupted by
// SIGINT/SIGTERM or ctx is cancelled, at which point it returns nil (a
// clean exit). It tries SSE (GET /v1/events/stream) first; if that
// connection cannot be established (or drops), it falls back to polling
// GET /v1/events every followPollInterval, picking up from wherever the SSE
// attempt left off.
func followEvents(ctx context.Context, c *client.Client, w io.Writer, session string, lastID int64) error {
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	newLastID, err := followEventsSSE(sigCtx, c, w, session, lastID)
	if err == nil || sigCtx.Err() != nil {
		return nil
	}
	return followEventsPoll(sigCtx, c, w, session, newLastID)
}

// followEventsSSE connects to GET /v1/events/stream?since=<lastID> and
// prints events as they arrive. It returns the id of the last event
// printed (so a caller falling back to polling can resume from there) and
// any error that ended the stream (nil on a clean ctx cancellation).
func followEventsSSE(ctx context.Context, c *client.Client, w io.Writer, session string, lastID int64) (int64, error) {
	q := url.Values{}
	q.Set("since", fmt.Sprintf("%d", lastID))
	if session != "" {
		q.Set("session", session)
	}

	err := c.Stream(ctx, "/v1/events/stream?"+q.Encode(), func(id int64, event, data string) error {
		var payload sseEventPayload
		if jsonErr := json.Unmarshal([]byte(data), &payload); jsonErr != nil {
			// Malformed frame (or a comment slipping through) — skip it
			// rather than aborting the whole stream.
			return nil
		}
		printEvents(w, []eventRow{{
			ID:        payload.ID,
			TS:        payload.TS,
			Type:      payload.Type,
			SessionID: payload.SessionID,
			Data:      payload.Data,
		}}, flags.JSON)
		lastID = payload.ID
		return nil
	})
	return lastID, err
}

// followEventsPoll polls the daemon for new events (id > lastID) every
// followPollInterval until interrupted (ctx done), at which point it
// returns nil (a clean exit).
func followEventsPoll(ctx context.Context, c *client.Client, w io.Writer, session string, lastID int64) error {
	ticker := time.NewTicker(followPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
