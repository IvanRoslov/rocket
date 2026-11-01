package queue

// Opt-in stock-Pi queue E2E for task5906 (Plan3 Task3). The ordinary suite
// skips this test unless ROCKET_PI_STOCK_E2E=1; with the flag set, any missing
// prerequisite fails instead of skipping. Everything the test needs is local
// to this file.

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	piagent "github.com/IvanRoslov/rocket/internal/agent/pi"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

const piStockE2EEnv = "ROCKET_PI_STOCK_E2E"

type piStockFixture struct {
	PiVersion  string `json:"pi_version"`
	AgentHome  string `json:"agent_home"`
	Cwd        string `json:"cwd"`
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	Endpoint   string `json:"endpoint"`
	InputLog   string `json:"input_log"`
	ProbeLog   string `json:"probe_log"`
	PiPID      int    `json:"pi_pid"`
}

func startPiStockFixture(t *testing.T) piStockFixture {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("stock E2E requires python3: %v", err)
	}
	fixture, err := filepath.Abs(filepath.Join("..", "..", "scripts", "probes", "tmux-delivary", "pi_queue_e2e_fixture.py"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("stock E2E fixture missing: %v", err)
	}
	cmd := exec.Command(python, fixture)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stock E2E fixture: %v", err)
	}

	type lineResult struct {
		line string
		err  error
	}
	lineCh := make(chan lineResult, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		lineCh <- lineResult{line: line, err: err}
	}()
	var line string
	select {
	case result := <-lineCh:
		if result.err != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			t.Fatalf("fixture did not announce readiness: %v; stderr: %s", result.err, stderr.String())
		}
		line = result.line
	case <-time.After(30 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("fixture readiness timed out; stderr: %s", stderr.String())
	}
	var info piStockFixture
	if err := json.Unmarshal([]byte(line), &info); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("decode fixture announcement: %v (%q)", err, line)
	}
	if info.PiVersion != "1.4.0" || info.AgentHome == "" || info.Cwd == "" || info.SessionID == "" ||
		info.InstanceID == "" || info.Endpoint == "" || info.InputLog == "" || info.PiPID <= 0 {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatalf("incomplete fixture identity: %+v", info)
	}

	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = syscall.Kill(info.PiPID, syscall.SIGKILL)
			<-done
		}
		if t.Failed() {
			t.Logf("fixture stderr: %s", stderr.String())
		}
	})
	return info
}

func piStockAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

type piStockRuntime struct {
	mu       sync.Mutex
	injects  int
	sendKeys int
	captures int
	escapes  int
}

func (r *piStockRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (r *piStockRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	r.mu.Lock()
	r.injects++
	r.mu.Unlock()
	return nil
}
func (r *piStockRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	r.mu.Lock()
	r.sendKeys++
	r.mu.Unlock()
	return nil
}
func (r *piStockRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.mu.Lock()
	r.captures++
	r.mu.Unlock()
	return "", nil
}
func (r *piStockRuntime) CaptureEscaped(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.mu.Lock()
	r.escapes++
	r.mu.Unlock()
	return "", nil
}
func (r *piStockRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (r *piStockRuntime) Destroy(ctx context.Context, h runtime.Handle) error {
	return nil
}
func (r *piStockRuntime) AttachCommand(h runtime.Handle) []string { return nil }
func (r *piStockRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, cols, rows int) error {
	return nil
}
func (r *piStockRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }
func (r *piStockRuntime) ScrollHistory(ctx context.Context, h runtime.Handle, lines int) error {
	return nil
}
func (r *piStockRuntime) ExitHistory(ctx context.Context, h runtime.Handle) error { return nil }
func (r *piStockRuntime) List(ctx context.Context) ([]string, error)              { return nil, nil }

func (r *piStockRuntime) tmuxCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.injects + r.sendKeys + r.captures + r.escapes
}

// piStockSender counts NativeSender invocations and delegates to the real
// UDS client, so a dedupe hit cannot hide an extra dial/request.
type piStockSender struct {
	mu  sync.Mutex
	ids []int64
}

func (s *piStockSender) Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
	s.mu.Lock()
	s.ids = append(s.ids, messageID)
	s.mu.Unlock()
	return nativeinput.Send(ctx, target, messageID, text)
}

func (s *piStockSender) counts() map[int64]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[int64]int)
	for _, id := range s.ids {
		counts[id]++
	}
	return counts
}

type piStockInputEvent struct {
	Event  string `json:"event"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

func piStockInputEvents(t *testing.T, path string) []piStockInputEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Pi input log: %v", err)
	}
	var events []piStockInputEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event piStockInputEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode Pi input log line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

// piStockSessionFile mirrors the production default private layout that the
// real Pi confirmer reads: <home>/sessions/--<encoded physical cwd>--/<date>_<id>.jsonl.
func piStockSessionFile(t *testing.T, home, cwd, sessionID string, wait time.Duration) string {
	t.Helper()
	physical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatalf("resolve physical cwd: %v", err)
	}
	encoded := strings.TrimPrefix(filepath.Clean(physical), string(filepath.Separator))
	encoded = strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(encoded)
	dir := filepath.Join(home, "sessions", "--"+encoded+"--")
	deadline := time.Now().Add(wait)
	for {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
					continue
				}
				_, id, ok := strings.Cut(entry.Name(), "_")
				if ok && strings.TrimSuffix(id, ".jsonl") == sessionID {
					return filepath.Join(dir, entry.Name())
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("session file for %s not found under %s", sessionID, dir)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func piStockUserRecordTexts(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Pi transcript: %v", err)
	}
	var texts []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode transcript line: %v", err)
		}
		if entry.Type != "message" {
			continue
		}
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(entry.Message, &message); err != nil || message.Role != "user" {
			// Non-user records and shapes the production parser ignores.
			continue
		}
		var content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(message.Content, &content); err != nil {
			// Plain-string content is not a text-part array; the production
			// userText parser ignores it too.
			continue
		}
		var parts []string
		for _, part := range content {
			if part.Type == "text" {
				parts = append(parts, part.Text)
			}
		}
		texts = append(texts, piStockNormalize(strings.Join(parts, "\n")))
	}
	return texts
}

func piStockNormalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func piStockStatus(t *testing.T, st *store.Store, id int64) string {
	t.Helper()
	msg, err := st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage(%d): %v", id, err)
	}
	return msg.Status
}

func piStockWait(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func piStockCount(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if text == want {
			n++
		}
	}
	return n
}

func TestPiNativeQueueStockE2E(t *testing.T) {
	if os.Getenv(piStockE2EEnv) != "1" {
		t.Skip("set " + piStockE2EEnv + "=1 to run the stock Pi queue E2E")
	}

	info := startPiStockFixture(t)
	t.Setenv("PI_CODING_AGENT_DIR", info.AgentHome)
	if !piStockAlive(info.PiPID) {
		t.Fatalf("Pi is not alive after fixture readiness")
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "stock-e2e.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	b := bus.New(st)
	rt := &piStockRuntime{}
	cfg := &config.Config{QueueTimeout: 30 * time.Minute, LargeMessageThreshold: 2048, SocketDelivery: true}
	q := New(st, b, rt, cfg, func(string) (activity.State, bool) { return activity.Ready, true })
	q.SetAgentResolver(func(string) (agent.Agent, error) { return piagent.New(), nil })
	sender := &piStockSender{}
	q.SetNativeSender(sender)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		piStockWait(t, 5*time.Second, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.workers) == 0
		}, "delivery workers to exit")
	})

	if err := st.AddSession(store.Session{
		ID: info.SessionID, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "pi",
		Branch: "main", WorktreePath: info.Cwd, TmuxName: "e2e", State: "running",
		DeliveryTransport: NativeTransportPiUDS, DeliveryInstance: info.InstanceID, DeliveryEndpoint: info.Endpoint,
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	q.Recover(ctx)
	go q.Run(ctx)

	const body = "E2E одинаковый исходный Body"
	aID, err := st.AddMessage(store.Message{FromSession: "e2e-sender", ToSession: info.SessionID, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	bID, err := st.AddMessage(store.Message{FromSession: "e2e-sender", ToSession: info.SessionID, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if aID == bID {
		t.Fatalf("A and B share the Rocket ID %d", aID)
	}
	q.Wake(info.SessionID)

	piStockWait(t, 30*time.Second, func() bool {
		return piStockStatus(t, st, bID) == "delivered" && piStockStatus(t, st, aID) == "accepted"
	}, "B delivered by its own record while A stays accepted")
	if !piStockAlive(info.PiPID) {
		t.Fatalf("Pi died before B could be proven")
	}

	aMsg, _ := st.GetMessage(aID)
	bMsg, _ := st.GetMessage(bID)
	if aMsg.Body != body || bMsg.Body != body {
		t.Fatalf("original Body changed: A=%q B=%q", aMsg.Body, bMsg.Body)
	}
	envelopeA := nativePiEnvelope(aID, formatBody(aMsg))
	envelopeB := nativePiEnvelope(bID, formatBody(bMsg))
	if aMsg.DeliveryProofAt != 0 || aMsg.DeliveryProofText != "" {
		t.Fatalf("A carries proof while it must stay unconfirmed: %+v", aMsg)
	}
	if bMsg.DeliveryProofAt == 0 || bMsg.DeliveryProofText != piStockNormalize(envelopeB) {
		t.Fatalf("B proof = %d/%q, want its own signed envelope %q", bMsg.DeliveryProofAt, bMsg.DeliveryProofText, piStockNormalize(envelopeB))
	}
	if got := sender.counts(); got[aID] != 1 || got[bID] != 1 || len(got) != 2 {
		t.Fatalf("NativeSender invocations = %v, want exactly one per ID", got)
	}

	piStockWait(t, 5*time.Second, func() bool {
		return len(piStockInputEvents(t, info.InputLog)) >= 2
	}, "both bridge API inputs logged")
	events := piStockInputEvents(t, info.InputLog)
	apiA, apiB, handled, continued := 0, 0, 0, 0
	for _, event := range events {
		switch event.Event {
		case "input":
			if event.Source != "extension" {
				t.Fatalf("unexpected input source %q", event.Source)
			}
			switch piStockNormalize(event.Text) {
			case piStockNormalize(envelopeA):
				apiA++
			case piStockNormalize(envelopeB):
				apiB++
			}
		case "handled":
			handled++
		case "continued":
			continued++
		}
	}
	if apiA != 1 || apiB != 1 || handled != 1 || continued != 1 {
		t.Fatalf("bridge API calls: A=%d B=%d handled=%d continued=%d, want 1/1/1/1", apiA, apiB, handled, continued)
	}

	sessionFile := piStockSessionFile(t, info.AgentHome, info.Cwd, info.SessionID, 10*time.Second)
	records := piStockUserRecordTexts(t, sessionFile)
	if got := piStockCount(records, piStockNormalize(envelopeA)); got != 0 {
		t.Fatalf("handled A left %d user records, want 0", got)
	}
	if got := piStockCount(records, piStockNormalize(envelopeB)); got != 1 {
		t.Fatalf("B left %d user records, want exactly 1 (all user records: %q)", got, records)
	}

	// Test-only synchronized clock offset; the config stays untouched. Pi is
	// alive and B is proven before this point.
	q.now = func() time.Time { return time.Now().Add(31 * time.Minute) }
	q.kickConfirmer()
	piStockWait(t, 10*time.Second, func() bool {
		return piStockStatus(t, st, aID) == "failed"
	}, "A to reach the existing timeout failure")
	terminalA, _ := st.GetMessage(aID)
	if terminalA.Reason != nativeDeliveryUnconfirmed || terminalA.DeliveryProofAt != 0 {
		t.Fatalf("terminal A = %+v, want failed/%s without proof", terminalA, nativeDeliveryUnconfirmed)
	}
	if piStockStatus(t, st, bID) != "delivered" {
		t.Fatal("B lost its delivered status after the timeout pass")
	}
	if !piStockAlive(info.PiPID) {
		t.Fatal("Pi was not alive at the end of the run")
	}
	if got := sender.counts(); got[aID] != 1 || got[bID] != 1 || len(got) != 2 {
		t.Fatalf("NativeSender invocations after timeout = %v, want no resend", got)
	}
	if got := rt.tmuxCalls(); got != 0 {
		t.Fatalf("visual/tmux calls on the native route = %d, want 0", got)
	}

	t.Logf("stock Pi E2E: pi=%s A=%d(%s) B=%d(%s proof_at=%d) sender=%v api=A%d/B%d handled=%d continued=%d records=A%d/B%d tmux=%d",
		info.PiVersion, aID, terminalA.Status, bID, piStockStatus(t, st, bID), bMsg.DeliveryProofAt,
		sender.counts(), apiA, apiB, handled, continued,
		piStockCount(records, piStockNormalize(envelopeA)), piStockCount(records, piStockNormalize(envelopeB)), rt.tmuxCalls())
}
