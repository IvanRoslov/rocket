package claudeinstance

import (
	"context"
	"errors"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"testing"
	"time"
)

type fakeProbe struct {
	pane      Pane
	processes map[int]Process
	err       error
}

func (p fakeProbe) Pane(context.Context, string) (Pane, bool, error) {
	return p.pane, p.pane.PID != 0, p.err
}
func (p fakeProbe) Process(_ context.Context, pid int) (Process, bool, error) {
	v, ok := p.processes[pid]
	return v, ok, p.err
}
func fixture() (Launch, []socketmsg.Session, fakeProbe) {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	l := Launch{RocketSessionID: "r", Generation: "g", PaneIdentity: "r:@1.%2", PanePID: 10, PaneStart: at, StartedAt: at.Add(-time.Second)}
	r := []socketmsg.Session{{PID: 11, SessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", Tmux: l.PaneIdentity, Kind: "interactive", PeerProtocol: 1, MessagingSocketPath: "/tmp/fake.sock", CWD: "/tmp/w", ProcStart: at.Format(time.RFC3339)}}
	p := fakeProbe{pane: Pane{Identity: l.PaneIdentity, PID: 10, Start: at}, processes: map[int]Process{10: {PID: 10, PPID: 1, Start: at, Name: "zsh"}, 11: {PID: 11, PPID: 10, Start: at, Name: "claude"}}}
	return l, r, p
}
func TestResolveExactRoot(t *testing.T) {
	l, r, p := fixture()
	got := Resolve(context.Background(), l, nil, r, p, "/tmp/projects")
	if got.State != Verified || got.Identity.PID != 11 || got.Identity.JournalPath != "/tmp/projects/-tmp-w/329baa3b-27eb-40ed-82e1-1d9f3dde01e5.jsonl" {
		t.Fatalf("unique root: %+v", got)
	}
	pinned := got.Identity
	for _, tc := range []struct {
		name   string
		mutate func(*Launch, *[]socketmsg.Session, *fakeProbe, *Identity)
		want   Liveness
	}{
		{"wrong pane", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { (*r)[0].Tmux = "r:@1.%9" }, Rejected},
		{"wrong UUID", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { i.ClaudeSessionID = "foreign" }, Rejected},
		{"wrong generation", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { l.Generation = "new" }, Rejected},
		{"nested root", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) {
			p.processes[10] = Process{PID: 10, Start: l.PaneStart, Name: "claude"}
		}, Rejected},
		{"PID reuse", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) {
			v := p.processes[11]
			v.Start = v.Start.Add(time.Second)
			p.processes[11] = v
		}, Dead},
		{"protocol", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { (*r)[0].PeerProtocol = 2 }, Rejected},
		{"ambiguous", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { *r = append(*r, (*r)[0]) }, Rejected},
		{"process error", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { p.err = errors.New("ps failed") }, Unknown},
		{"root dead", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { delete(p.processes, 11) }, Dead},
		{"legacy unproven", func(l *Launch, r *[]socketmsg.Session, p *fakeProbe, i *Identity) { l.PanePID = 0 }, Rejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, r, p := fixture()
			i := pinned
			tc.mutate(&l, &r, &p, &i)
			got := Resolve(context.Background(), l, &i, r, p, "/tmp/projects")
			if got.State != tc.want {
				t.Fatalf("got %+v, want %s", got, tc.want)
			}
		})
	}
}
func TestResolveNormalizesTimezone(t *testing.T) {
	l, r, p := fixture()
	r[0].ProcStart = "2026-11-01T03:00:00+03:00"
	if got := Resolve(context.Background(), l, nil, r, p, "/tmp/projects"); got.State != Verified {
		t.Fatalf("same instant: %+v", got)
	}
}

func TestJournalSlugPreservesOtherPathCharacters(t *testing.T) {
	l, r, p := fixture()
	r[0].CWD = "/work/project_name.v2"
	got := Resolve(context.Background(), l, nil, r, p, "/tmp/projects")
	if got.State != Verified || got.Identity.JournalPath != "/tmp/projects/-work-project_name-v2/329baa3b-27eb-40ed-82e1-1d9f3dde01e5.jsonl" {
		t.Fatalf("wrong exact journal path: %+v", got)
	}
}
