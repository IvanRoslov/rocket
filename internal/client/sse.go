package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Stream performs a GET request to path with "Accept: text/event-stream",
// then parses the response body as a Server-Sent Events stream, invoking
// handler once per event.
//
// Unlike Get/Post/Patch/Delete, Stream does not apply defaultRequestTimeout:
// a streaming connection is meant to live for as long as the caller wants
// it to. Callers control its lifetime entirely through ctx (e.g. cancelling
// on SIGINT).
//
// Stream returns when handler returns an error (that error is returned
// as-is), when the connection is closed by the peer (io.EOF, returned as
// nil), or when ctx is cancelled (the context's error is returned).
func (c *Client) Stream(ctx context.Context, path string, handler func(id int64, event, data string) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	return parseSSE(resp.Body, handler)
}

// parseSSE reads Server-Sent Events frames from r, invoking handler once
// per complete event (a run of "id:"/"event:"/"data:" lines terminated by a
// blank line). Lines starting with ':' are comments (e.g. heartbeat pings)
// and are ignored. It returns nil on a clean EOF, or the first error
// returned by handler, or a scanner error.
func parseSSE(r io.Reader, handler func(id int64, event, data string) error) error {
	scanner := bufio.NewScanner(r)
	// SSE data lines (a JSON-encoded event) can exceed bufio.Scanner's
	// default 64KiB token limit for large event payloads; allow up to 1MiB.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var id int64
	var event, data string
	haveAny := false

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if !haveAny {
				continue
			}
			if err := handler(id, event, data); err != nil {
				return err
			}
			id, event, data = 0, "", ""
			haveAny = false
		case strings.HasPrefix(line, ":"):
			// Comment (e.g. ": ping" heartbeat) — ignored.
		case strings.HasPrefix(line, "id:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				id = n
			}
			haveAny = true
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			haveAny = true
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			haveAny = true
		}
	}
	return scanner.Err()
}
