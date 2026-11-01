package claudeinstance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExactSavedRootLiveness(t *testing.T) {
	l, r, p := fixture()
	id := Resolve(context.Background(), l, nil, r, p, "/tmp/p").Identity
	if got := ExactLiveness(context.Background(), id, p); got != Verified {
		t.Fatalf("live root=%s", got)
	}
	reused := p.processes[id.PID]
	reused.Start = reused.Start.Add(time.Second)
	p.processes[id.PID] = reused
	if got := ExactLiveness(context.Background(), id, p); got != Dead {
		t.Fatalf("reused root=%s", got)
	}
	delete(p.processes, id.PID)
	if got := ExactLiveness(context.Background(), id, p); got != Dead {
		t.Fatalf("dead root=%s", got)
	}
	p.err = errors.New("ps failed")
	if got := ExactLiveness(context.Background(), id, p); got != Unknown {
		t.Fatalf("probe error=%s", got)
	}
}
