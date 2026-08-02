package agentwatch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// --- fakes ---------------------------------------------------------------

// fakeRuntime reports a fixed set of live tmux session names. Only List is
// exercised; the rest of the interface is inert.
type fakeRuntime struct {
	names   []string
	listErr error
}

func (f *fakeRuntime) Create(context.Context, runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{}, nil
}
func (f *fakeRuntime) Inject(context.Context, runtime.Handle, string) error          { return nil }
func (f *fakeRuntime) SendKeys(context.Context, runtime.Handle, string, bool) error  { return nil }
func (f *fakeRuntime) Capture(context.Context, runtime.Handle, int) (string, error)  { return "", nil }
func (f *fakeRuntime) Alive(context.Context, runtime.Handle) bool                    { return false }
func (f *fakeRuntime) Destroy(context.Context, runtime.Handle) error                 { return nil }
func (f *fakeRuntime) AttachCommand(runtime.Handle) []string                         { return nil }
func (f *fakeRuntime) PinWindowSize(context.Context, runtime.Handle, int, int) error { return nil }
func (f *fakeRuntime) UnpinWindowSize(context.Context, runtime.Handle) error         { return nil }
func (f *fakeRuntime) List(context.Context) ([]string, error)                        { return f.names, f.listErr }

// fakeSessions records adoption and retirement instead of writing session rows.
type fakeSessions struct {
	adopted   []string
	retired   []string
	adoptErr  error
	adoptedOK map[string]bool
}

func (f *fakeSessions) AdoptAgentSession(a store.Agent) (store.Session, error) {
	if f.adoptErr != nil {
		return store.Session{}, f.adoptErr
	}
	f.adopted = append(f.adopted, a.ID)
	if f.adoptedOK == nil {
		f.adoptedOK = map[string]bool{}
	}
	f.adoptedOK[a.ID] = true
	return store.Session{ID: a.ID, Kind: "agent", State: "running"}, nil
}

func (f *fakeSessions) RetireAgentSession(id string) error {
	f.retired = append(f.retired, id)
	return nil
}

// --- scaffolding ---------------------------------------------------------

type harness struct {
	w     *Watcher
	st    *store.Store
	rt    *fakeRuntime
	sess  *fakeSessions
	woken []string
	now   time.Time
}

func newHarness(t *testing.T, agents ...string) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	for _, id := range agents {
		if err := st.AddAgent(store.Agent{ID: id, Enabled: true}); err != nil {
			t.Fatalf("AddAgent %s: %v", id, err)
		}
	}

	h := &harness{
		st:   st,
		rt:   &fakeRuntime{},
		sess: &fakeSessions{},
		now:  time.Unix(1_800_000_000, 0),
	}
	cfg := &config.Config{
		ActivityPollInterval: time.Second,
		AgentNotifyInterval:  5 * time.Minute,
	}
	h.w = New(st, h.rt, cfg, h.sess, func(to string) { h.woken = append(h.woken, to) })
	h.w.now = func() time.Time { return h.now }
	return h
}

// unread appends an unread message to an agent's inbox.
func (h *harness) unread(t *testing.T, agentID, body string) {
	t.Helper()
	if _, err := h.st.AddInboxMessage(store.InboxMessage{AgentID: agentID, Body: body}); err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}
}

// notices returns the notification messages queued to an agent.
func (h *harness) notices(t *testing.T, agentID string) []string {
	t.Helper()
	msgs, err := h.st.ListMessages(agentID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	var out []string
	for _, m := range msgs {
		out = append(out, m.Body)
	}
	return out
}

// --- tests ---------------------------------------------------------------

func TestTickAdoptsLiveSessionsAndRetiresGoneOnes(t *testing.T) {
	h := newHarness(t, "sre", "triage")
	h.rt.names = []string{"sre", "billing-orch"}

	h.w.Tick(context.Background())

	if len(h.sess.adopted) != 1 || h.sess.adopted[0] != "sre" {
		t.Errorf("adopted = %v, want [sre]", h.sess.adopted)
	}
	if len(h.sess.retired) != 1 || h.sess.retired[0] != "triage" {
		t.Errorf("retired = %v, want [triage]", h.sess.retired)
	}
}

func TestTickSkipsDisabledAgents(t *testing.T) {
	h := newHarness(t, "sre")
	if err := h.st.UpdateAgent(store.Agent{ID: "sre", Enabled: false}); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "hi")

	h.w.Tick(context.Background())

	if len(h.sess.adopted) != 0 {
		t.Errorf("adopted = %v, want none for a disabled agent", h.sess.adopted)
	}
	if n := h.notices(t, "sre"); len(n) != 0 {
		t.Errorf("notices = %v, want none for a disabled agent", n)
	}
}

func TestTickSkipsEverythingWhenTmuxCannotBeListed(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.listErr = errors.New("tmux is not running")

	h.w.Tick(context.Background())

	if len(h.sess.adopted) != 0 || len(h.sess.retired) != 0 {
		t.Errorf("adopted = %v, retired = %v, want neither", h.sess.adopted, h.sess.retired)
	}
}

func TestNotifiesOnceWhenTheSessionAppears(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "one")
	h.unread(t, "sre", "two")

	h.w.Tick(context.Background())

	notices := h.notices(t, "sre")
	if len(notices) != 1 {
		t.Fatalf("notices = %v, want exactly one", notices)
	}
	for _, want := range []string{"2 unread messages", "rocket inbox next"} {
		if !strings.Contains(notices[0], want) {
			t.Errorf("notice = %q, missing %q", notices[0], want)
		}
	}
	if len(h.woken) != 1 || h.woken[0] != "sre" {
		t.Errorf("woken = %v, want [sre]", h.woken)
	}

	// A second tick over the same unread pile stays quiet.
	h.now = h.now.Add(time.Hour)
	h.w.Tick(context.Background())
	if notices := h.notices(t, "sre"); len(notices) != 1 {
		t.Errorf("notices after a quiet tick = %v, want still one", notices)
	}
}

func TestSingularWording(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "one")

	h.w.Tick(context.Background())

	notices := h.notices(t, "sre")
	if len(notices) != 1 || !strings.Contains(notices[0], "1 unread message.") {
		t.Fatalf("notice = %v, want the singular wording", notices)
	}
}

func TestNoNotificationWithoutUnread(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}

	h.w.Tick(context.Background())

	if notices := h.notices(t, "sre"); len(notices) != 0 {
		t.Errorf("notices = %v, want none", notices)
	}
}

func TestNewUnreadRenotifiesOnlyAfterTheInterval(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "one")
	h.w.Tick(context.Background())

	// New mail, but too soon after the first notice.
	h.unread(t, "sre", "two")
	h.now = h.now.Add(time.Minute)
	h.w.Tick(context.Background())
	if notices := h.notices(t, "sre"); len(notices) != 1 {
		t.Fatalf("notices inside the anti-spam window = %v, want still one", notices)
	}

	// Past the interval, the new message earns a fresh notice.
	h.now = h.now.Add(5 * time.Minute)
	h.w.Tick(context.Background())
	notices := h.notices(t, "sre")
	if len(notices) != 2 {
		t.Fatalf("notices after the interval = %v, want two", notices)
	}
	if !strings.Contains(notices[1], "2 unread messages") {
		t.Errorf("second notice = %q, want the current unread count", notices[1])
	}
}

func TestSessionRestartNotifiesAgain(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "one")
	h.w.Tick(context.Background())

	// The agent's session goes away and comes back with the same mail still
	// unread: the new session has not been told about it.
	h.rt.names = nil
	h.w.Tick(context.Background())
	h.rt.names = []string{"sre"}
	h.now = h.now.Add(time.Second)
	h.w.Tick(context.Background())

	if notices := h.notices(t, "sre"); len(notices) != 2 {
		t.Fatalf("notices = %v, want one per session", notices)
	}
}

func TestAdoptionFailureSkipsTheAgent(t *testing.T) {
	h := newHarness(t, "sre")
	h.rt.names = []string{"sre"}
	h.unread(t, "sre", "one")
	h.sess.adoptErr = errors.New("session sre is an orchestrator session")

	h.w.Tick(context.Background())

	if notices := h.notices(t, "sre"); len(notices) != 0 {
		t.Errorf("notices = %v, want none when the session cannot be adopted", notices)
	}
}
