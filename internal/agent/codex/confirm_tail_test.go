package codex

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// countingReaderAt counts the bytes served, to prove a scan stayed in the
// tail instead of reading the whole journal.
type countingReaderAt struct {
	r    *bytes.Reader
	read int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

func userLine(ts, text string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`, ts, text)
}

func eventLine(ts, payload string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"exec_command_end","aggregated_output":%q}}`, ts, payload)
}

func scanAll(t *testing.T, journal string, since string) (visited []string, read int64) {
	t.Helper()
	cr := &countingReaderAt{r: bytes.NewReader([]byte(journal))}
	err := scanTailSince(context.Background(), cr, int64(len(journal)), ts(t, since), func(line []byte) {
		visited = append(visited, string(line))
	})
	if err != nil {
		t.Fatal(err)
	}
	return visited, cr.read
}

func TestScanTailStopsAtSinceAndSkipsOldBulk(t *testing.T) {
	// 2 MiB of records older than since, then a handful newer: the scan must
	// visit exactly the newer ones (newest first) without reading the bulk.
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString(eventLine("2026-10-28T12:00:00.000Z", strings.Repeat("x", 10<<10)))
		sb.WriteString("\n")
	}
	bulk := int64(sb.Len())
	sb.WriteString(userLine("2026-10-28T12:10:00.000Z", "at since exactly") + "\n")
	sb.WriteString(userLine("2026-10-28T12:10:01.000Z", "new one") + "\n")
	sb.WriteString(userLine("2026-10-28T12:10:02.000Z", "new two") + "\n")

	visited, read := scanAll(t, sb.String(), "2026-10-28T12:10:00Z")
	if len(visited) != 2 || !strings.Contains(visited[0], "new two") || !strings.Contains(visited[1], "new one") {
		t.Fatalf("visited = %d lines: %q", len(visited), visited)
	}
	if read >= bulk {
		t.Fatalf("scan read %d bytes, bulk is %d: it did not stop at since", read, bulk)
	}
}

func TestScanTailLineWithoutTimestampDoesNotStopScan(t *testing.T) {
	journal := strings.Join([]string{
		userLine("2026-10-28T12:00:00.000Z", "old"),
		userLine("2026-10-28T12:10:01.000Z", "new one"),
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"no timestamp"}]}}`,
		`not json at all`,
		userLine("2026-10-28T12:10:02.000Z", "new two"),
	}, "\n") + "\n"
	visited, _ := scanAll(t, journal, "2026-10-28T12:10:00Z")
	if len(visited) != 4 {
		t.Fatalf("visited = %q", visited)
	}
	if !strings.Contains(visited[3], "new one") {
		t.Fatalf("scan stopped early: %q", visited)
	}
}

func TestScanTailLineSpansChunkBoundary(t *testing.T) {
	// A record longer than one read chunk, newer than since, is handed over
	// whole; a file without a trailing newline is handled too.
	long := userLine("2026-10-28T12:10:01.000Z", strings.Repeat("y", 3*tailChunk))
	journal := userLine("2026-10-28T12:00:00.000Z", "old") + "\n" + long + "\n" + userLine("2026-10-28T12:10:02.000Z", "last")
	visited, _ := scanAll(t, journal, "2026-10-28T12:10:00Z")
	if len(visited) != 2 || visited[1] != long || !strings.Contains(visited[0], "last") {
		t.Fatalf("visited = %d lines, long intact=%v", len(visited), len(visited) > 1 && visited[1] == long)
	}
}

func TestConfirmDeliveredHonorsCancelledContext(t *testing.T) {
	ref := installConfirmFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (&Codex{}).ConfirmDelivered(ctx, ref, "[from orch] first message ALPHA", ts(t, "2026-10-28T12:18:00Z")); err == nil {
		t.Fatal("want context error")
	}
	_ = time.Second
}

func TestScanTailRecordExactlyAtSinceIsNotVisited(t *testing.T) {
	journal := strings.Join([]string{
		userLine("2026-10-28T12:09:59.999Z", "before"),
		userLine("2026-10-28T12:10:00.000Z", "at since"),
		userLine("2026-10-28T12:10:00.001Z", "after"),
	}, "\n") + "\n"
	visited, _ := scanAll(t, journal, "2026-10-28T12:10:00Z")
	if len(visited) != 1 || !strings.Contains(visited[0], `"after"`) {
		t.Fatalf("visited = %q, want only the record after since", visited)
	}
}
