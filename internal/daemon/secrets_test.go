package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileSecrets(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "deepseek-api-key"), []byte("sk-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DEEPSEEK_API_KEY", "")
	loadFileSecrets(home)
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-file" {
		t.Errorf("empty env: DEEPSEEK_API_KEY = %q, want value from file", got)
	}

	t.Setenv("DEEPSEEK_API_KEY", "sk-env")
	loadFileSecrets(home)
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-env" {
		t.Errorf("set env: DEEPSEEK_API_KEY = %q, want the environment to win", got)
	}

	t.Setenv("DEEPSEEK_API_KEY", "")
	loadFileSecrets(t.TempDir())
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "" {
		t.Errorf("no file: DEEPSEEK_API_KEY = %q, want empty", got)
	}
}
