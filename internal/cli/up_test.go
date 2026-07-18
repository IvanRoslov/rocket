package cli

import (
	"errors"
	"testing"
)

// TestUpUsage tests usage violations for `rocket up`.
func TestUpUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"title", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newUpCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestUpDescMutuallyExclusive tests that --desc and --desc-file are
// mutually exclusive, mirroring `rocket task add`.
func TestUpDescMutuallyExclusive(t *testing.T) {
	cmd := newUpCmd()
	cmd.SetArgs([]string{"title", "--desc", "text", "--desc-file", "file.md"})
	err := cmd.Execute()
	if err == nil {
		t.Errorf("expected error for mutually exclusive flags")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Errorf("expected usageError, got %T: %v", err, err)
	}
}
