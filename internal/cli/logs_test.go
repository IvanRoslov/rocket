package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLastNLines tests the lastNLines function with various input scenarios.
func TestLastNLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		n        int
		expected []string
	}{
		{
			name:     "empty file",
			content:  "",
			n:        10,
			expected: []string{},
		},
		{
			name:     "fewer lines than n",
			content:  "line1\nline2\nline3\n",
			n:        10,
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "exactly n lines",
			content:  "line1\nline2\nline3\nline4\nline5\n",
			n:        5,
			expected: []string{"line1", "line2", "line3", "line4", "line5"},
		},
		{
			name:     "more lines than n",
			content:  "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n",
			n:        5,
			expected: []string{"line6", "line7", "line8", "line9", "line10"},
		},
		{
			name:     "no trailing newline",
			content:  "line1\nline2\nline3",
			n:        10,
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "n is zero",
			content:  "line1\nline2\nline3\n",
			n:        0,
			expected: nil,
		},
		{
			name:     "n is negative",
			content:  "line1\nline2\nline3\n",
			n:        -5,
			expected: nil,
		},
		{
			name:     "single line file",
			content:  "oneline\n",
			n:        1,
			expected: []string{"oneline"},
		},
		{
			name:     "single line without newline",
			content:  "oneline",
			n:        1,
			expected: []string{"oneline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.content)
			result, err := lastNLines(r, tt.n)
			if err != nil {
				t.Fatalf("lastNLines failed: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("got %d lines, expected %d", len(result), len(tt.expected))
			}
			for i, line := range result {
				if i >= len(tt.expected) {
					t.Errorf("unexpected line at index %d: %q", i, line)
					continue
				}
				if line != tt.expected[i] {
					t.Errorf("line %d: got %q, expected %q", i, line, tt.expected[i])
				}
			}
		})
	}
}

// TestTailFileEndToEnd tests the tailFile function end-to-end with a real file.
func TestTailFileEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Create a file with 150 lines
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	for i := 1; i <= 150; i++ {
		_, err := io.WriteString(f, "line"+string(rune(i))+"\n")
		if err != nil {
			t.Fatalf("failed to write line: %v", err)
		}
	}
	f.Close()

	// Tail the last 100 lines
	var buf bytes.Buffer
	size, err := tailFile(logPath, 100, &buf)
	if err != nil {
		t.Fatalf("tailFile failed: %v", err)
	}

	// Verify the size is correct
	if size <= 0 {
		t.Errorf("size should be > 0, got %d", size)
	}

	// Verify we got 100 lines (51-150)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 100 {
		t.Errorf("got %d lines, expected 100", len(lines))
	}

	// Verify the content is lines 51-150
	for i := 0; i < 100; i++ {
		lineNum := 51 + i
		expected := "line" + string(rune(lineNum))
		if lines[i] != expected {
			t.Errorf("line %d: got %q, expected %q", i, lines[i], expected)
		}
	}
}

// TestTailFileMissingFile tests that tailFile gracefully handles a missing file.
func TestTailFileMissingFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nonexistent.log")

	var buf bytes.Buffer
	size, err := tailFile(logPath, 10, &buf)
	if err != nil {
		t.Fatalf("tailFile failed: %v", err)
	}

	if size != 0 {
		t.Errorf("size should be 0 for missing file, got %d", size)
	}
	if buf.Len() > 0 {
		t.Errorf("buffer should be empty for missing file, got %q", buf.String())
	}
}

// TestReadGrowthAppend tests readGrowth when the file has been appended to.
func TestReadGrowthAppend(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Create a file with initial content
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	initial := "line1\nline2\nline3\n"
	_, err = io.WriteString(f, initial)
	if err != nil {
		t.Fatalf("failed to write initial content: %v", err)
	}
	f.Close()

	// Read initial growth (should be full content since starting from 0)
	var buf1 bytes.Buffer
	offset1, err := readGrowth(logPath, 0, &buf1)
	if err != nil {
		t.Fatalf("first readGrowth failed: %v", err)
	}

	if buf1.String() != initial {
		t.Errorf("got %q, expected %q", buf1.String(), initial)
	}

	// Append more content
	f, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	appended := "line4\nline5\n"
	_, err = io.WriteString(f, appended)
	if err != nil {
		t.Fatalf("failed to write appended content: %v", err)
	}
	f.Close()

	// Read growth from the previous offset
	var buf2 bytes.Buffer
	offset2, err := readGrowth(logPath, offset1, &buf2)
	if err != nil {
		t.Fatalf("second readGrowth failed: %v", err)
	}

	// Should only get the appended content
	if buf2.String() != appended {
		t.Errorf("got %q, expected %q", buf2.String(), appended)
	}

	// Offset should have advanced
	if offset2 <= offset1 {
		t.Errorf("offset should advance: %d -> %d", offset1, offset2)
	}
}

// TestReadGrowthRotation tests readGrowth when the file is rotated/truncated.
func TestReadGrowthRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Create a file with initial content
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	initial := "line1\nline2\nline3\nline4\nline5\n"
	_, err = io.WriteString(f, initial)
	if err != nil {
		t.Fatalf("failed to write initial content: %v", err)
	}
	f.Close()

	// Read initial file and get offset
	var buf1 bytes.Buffer
	offset1, err := readGrowth(logPath, 0, &buf1)
	if err != nil {
		t.Fatalf("first readGrowth failed: %v", err)
	}

	if buf1.String() != initial {
		t.Errorf("initial content mismatch")
	}

	// Simulate rotation: truncate and write new content
	f, err = os.Create(logPath) // Create truncates the file
	if err != nil {
		t.Fatalf("failed to truncate file: %v", err)
	}
	newContent := "newline1\nnewline2\n"
	_, err = io.WriteString(f, newContent)
	if err != nil {
		t.Fatalf("failed to write new content: %v", err)
	}
	f.Close()

	// Read growth should detect that file is now smaller and reopen from 0
	var buf2 bytes.Buffer
	offset2, err := readGrowth(logPath, offset1, &buf2)
	if err != nil {
		t.Fatalf("second readGrowth failed: %v", err)
	}

	// Should get the full new content
	if buf2.String() != newContent {
		t.Errorf("got %q, expected %q", buf2.String(), newContent)
	}

	// Offset should be reset to file size (which is now smaller)
	if offset2 >= offset1 {
		t.Errorf("offset should reset: %d -> %d (old was larger)", offset1, offset2)
	}
}

// TestReadGrowthNoChange tests readGrowth when the file hasn't changed.
func TestReadGrowthNoChange(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Create a file with initial content
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer f.Close()

	content := "line1\nline2\nline3\n"
	_, err = io.WriteString(f, content)
	if err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	f.Close()

	// Get the file size
	fi, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	// Read from the end of the file (no new data)
	var buf bytes.Buffer
	offset, err := readGrowth(logPath, fi.Size(), &buf)
	if err != nil {
		t.Fatalf("readGrowth failed: %v", err)
	}

	// Should get nothing
	if buf.Len() > 0 {
		t.Errorf("expected empty buffer, got %q", buf.String())
	}

	// Offset should remain the same
	if offset != fi.Size() {
		t.Errorf("offset should not change: expected %d, got %d", fi.Size(), offset)
	}
}

// TestReadGrowthMissingFile tests readGrowth when the file doesn't exist.
func TestReadGrowthMissingFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nonexistent.log")

	var buf bytes.Buffer
	offset, err := readGrowth(logPath, 0, &buf)
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}

	// Offset should be returned unchanged
	if offset != 0 {
		t.Errorf("offset should be unchanged: expected 0, got %d", offset)
	}
}
