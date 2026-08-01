package agentrun

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// openTestStore opens a fresh store in a temp home and returns both, so tests
// can reach the role files the runtime reads (role.md, memory/MEMORY.md).
func openTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, home
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
