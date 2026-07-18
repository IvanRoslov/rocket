package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildSendBodyUsageViolations tests argument validation.
func TestBuildSendBodyUsageViolations(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("file content"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name     string
		args     []string
		filePath string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "no args",
			args:     []string{},
			filePath: "",
			wantErr:  true,
			errMsg:   "exactly one session id and one body source required",
		},
		{
			name:     "only session",
			args:     []string{"session-id"},
			filePath: "",
			wantErr:  true,
			errMsg:   "exactly one session id and one body source required",
		},
		{
			name:     "three args",
			args:     []string{"session-id", "arg2", "arg3"},
			filePath: "",
			wantErr:  true,
			errMsg:   "exactly one session id and one body source required",
		},
		{
			name:     "both text and --file",
			args:     []string{"session-id", "text"},
			filePath: testFile,
			wantErr:  true,
			errMsg:   "cannot use both positional body and --file",
		},
		{
			name:     "file not found",
			args:     []string{"session-id"},
			filePath: filepath.Join(tmpDir, "nonexistent.txt"),
			wantErr:  true,
			errMsg:   "no such file or directory",
		},
		{
			name:     "empty body text",
			args:     []string{"session-id", ""},
			filePath: "",
			wantErr:  true,
			errMsg:   "body must not be empty",
		},
		{
			name:     "valid with text",
			args:     []string{"session-id", "hello world"},
			filePath: "",
			wantErr:  false,
		},
		{
			name:     "valid with file",
			args:     []string{"session-id"},
			filePath: testFile,
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildSendBody(tc.args, tc.filePath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errMsg != "" && !containsSubstring(err.Error(), tc.errMsg) {
					t.Errorf("error message: got %q, want substring %q", err.Error(), tc.errMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if body == "" {
					t.Errorf("body should not be empty")
				}
			}
		})
	}
}

// TestBuildSendBodyContent tests that body content is read correctly.
func TestBuildSendBodyContent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "file content with\nmultiple lines\nand stuff"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cases := []struct {
		name     string
		args     []string
		filePath string
		want     string
	}{
		{
			name:     "text from args",
			args:     []string{"session-id", "hello world"},
			filePath: "",
			want:     "hello world",
		},
		{
			name:     "text with quotes",
			args:     []string{"session-id", "hello \"world\""},
			filePath: "",
			want:     "hello \"world\"",
		},
		{
			name:     "multiline text",
			args:     []string{"session-id", "line1\nline2\nline3"},
			filePath: "",
			want:     "line1\nline2\nline3",
		},
		{
			name:     "content from file",
			args:     []string{"session-id"},
			filePath: testFile,
			want:     content,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := buildSendBody(tc.args, tc.filePath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if body != tc.want {
				t.Errorf("got %q, want %q", body, tc.want)
			}
		})
	}
}

// containsSubstring checks if haystack contains needle as a substring.
func containsSubstring(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
