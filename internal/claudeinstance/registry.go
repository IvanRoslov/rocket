package claudeinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
)

// ReadRegistry is stricter than discovery for UI: a skipped, unreadable entry
// could be another writer of the exact UUID. Incomplete evidence cannot prove
// uniqueness. Bounds are conservative availability limits, never permission
// to skip an alternative. It reads metadata only and performs no live probe.
func ReadRegistry(ctx context.Context, dir string) ([]socketmsg.Session, error) {
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("Claude registry exceeds verification limit")
	}
	var all []socketmsg.Session
	for _, entry := range entries {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		pid, e := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if e != nil || pid <= 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Claude registry entry unverifiable")
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, fmt.Errorf("Claude registry entry unreadable")
		}
		actual, e := f.Stat()
		if e != nil || !os.SameFile(info, actual) {
			f.Close()
			return nil, fmt.Errorf("Claude registry entry changed")
		}
		raw, e := io.ReadAll(io.LimitReader(f, 64*1024+1))
		f.Close()
		if e != nil || len(raw) > 64*1024 {
			return nil, fmt.Errorf("Claude registry entry exceeds verification limit")
		}
		var s socketmsg.Session
		if json.Unmarshal(raw, &s) != nil || !uuidPattern.MatchString(s.SessionID) {
			return nil, fmt.Errorf("Claude registry identity incomplete")
		}
		s.PID = pid
		all = append(all, s)
	}
	return all, ctx.Err()
}
