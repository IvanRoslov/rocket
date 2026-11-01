package claudeinstance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
)

func TestStrictRegistryPreservesHeadlessAndRejectsIncompleteAlternatives(t *testing.T) {
	for _, which := range []string{"headless", "corrupt", "missing UUID", "symlink", "oversized", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "12.json")
			s := socketmsg.Session{SessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", Kind: "headless"}
			if which == "missing UUID" {
				s.SessionID = ""
			}
			raw, _ := json.Marshal(s)
			if which == "corrupt" {
				raw = []byte("{")
			}
			if which == "oversized" {
				raw = make([]byte, 65537)
			}
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if which == "symlink" {
				os.Rename(path, path+".real")
				os.Symlink(path+".real", path)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if which == "cancelled" {
				cancel()
			}
			got, e := ReadRegistry(ctx, dir)
			if which == "headless" {
				if e != nil || len(got) != 1 || got[0].PID != 12 {
					t.Fatalf("valid headless metadata rejected: %+v %v", got, e)
				}
			} else if e == nil {
				t.Fatal("incomplete registry proved uniqueness")
			}
		})
	}
}
