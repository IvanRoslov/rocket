package queue

import (
	"context"
	"errors"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
	"path/filepath"
	"time"
)

const ClaudeSocketTransport = "claude-socket-v1"

type SocketOutcome string

const (
	SocketNotSent  SocketOutcome = "not_sent"
	SocketAccepted SocketOutcome = "accepted"
	SocketHeld     SocketOutcome = "held"
	SocketUnknown  SocketOutcome = "unknown"
	SocketDenied   SocketOutcome = "denied"
	SocketExpired  SocketOutcome = "expired"
)

type SocketSender interface {
	Resolve(context.Context, store.Session) claudeinstance.Resolution
	Prepare(context.Context, store.Session, string, string) (claudeinstance.Snapshot, error)
	Send(context.Context, claudeinstance.Snapshot) (SocketOutcome, error)
}

var ErrNoSocket = errors.New("queue: exact Claude socket unavailable")
var ErrHeld = errors.New("queue: recipient held message")

const defaultHeldWait = 2 * time.Second

type ClaudeSocketSender struct {
	sessionsDir  string
	receipts     *socketmsg.Receipts
	st           *store.Store
	resolveAgent func(string) (agent.Agent, error)
	probe        claudeinstance.Probe
	heldWait     time.Duration
}

func NewClaudeSocketSender(dir string) *ClaudeSocketSender {
	return &ClaudeSocketSender{sessionsDir: dir, resolveAgent: agent.Get, probe: claudeinstance.SystemProbe{}, heldWait: defaultHeldWait}
}
func (s *ClaudeSocketSender) SetStore(st *store.Store) { s.st = st }
func (s *ClaudeSocketSender) SetAgentResolver(resolve func(string) (agent.Agent, error)) {
	s.resolveAgent = resolve
}
func (s *ClaudeSocketSender) SetReceipts(r *socketmsg.Receipts) {
	s.receipts = r
	if r != nil {
		r.SetHandler(s.handleReceipt)
	}
}
func (s *ClaudeSocketSender) Resolve(ctx context.Context, sess store.Session) claudeinstance.Resolution {
	rejected := func(reason string) claudeinstance.Resolution {
		return claudeinstance.Resolution{State: claudeinstance.Rejected, Reason: reason}
	}
	if s.st == nil || s.resolveAgent == nil {
		return rejected("missing_wiring")
	}
	a, e := s.resolveAgent(sess.Agent)
	if e != nil || !agent.SupportsClaudeSocket(a) {
		return rejected("incompatible_adapter")
	}
	l, pinned, e := s.st.GetClaudeLaunch(sess.ID)
	if e != nil {
		return rejected("unproven_launch")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	registry, e := socketmsg.ListSessions(s.sessionsDir)
	if e != nil {
		return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "registry_lookup"}
	}
	resolution := claudeinstance.Resolve(ctx, l, pinned, registry, s.probe, filepath.Join(filepath.Dir(s.sessionsDir), "projects"))
	if resolution.State == claudeinstance.Verified {
		ok, e := s.st.BindClaudeIdentity(resolution.Identity)
		if e != nil || !ok {
			return rejected("binding_persistence")
		}
	}
	return resolution
}
func (s *ClaudeSocketSender) Prepare(ctx context.Context, sess store.Session, fromName, text string) (claudeinstance.Snapshot, error) {
	result := s.Resolve(ctx, sess)
	if result.State != claudeinstance.Verified {
		return claudeinstance.Snapshot{}, ErrNoSocket
	}
	boundary, e := claudeinstance.PinJournal(result.Identity)
	if e != nil {
		return claudeinstance.Snapshot{}, e
	}
	wire, e := socketmsg.NewMsgID()
	if e != nil {
		return claudeinstance.Snapshot{}, e
	}
	reply := ""
	if s.receipts != nil {
		reply = s.receipts.Path(filepath.Dir(result.Identity.SocketPath))
	}
	from := ""
	if reply != "" {
		from = socketmsg.EncodeAddr(reply)
	}
	return claudeinstance.Snapshot{Transport: ClaudeSocketTransport, Target: result.Identity, WireID: wire, Envelope: socketmsg.Envelope(from, fromName, text), ReplyPath: reply, Boundary: boundary}, nil
}
func (s *ClaudeSocketSender) Send(ctx context.Context, v claudeinstance.Snapshot) (SocketOutcome, error) {
	if s.st == nil {
		return SocketNotSent, ErrNoSocket
	}
	saved, e := s.st.GetClaudeDelivery(v.MessageID)
	if e != nil || saved.WireID != v.WireID || !claudeinstance.SameIdentity(saved.Target, v.Target) || saved.Envelope != v.Envelope || saved.Boundary != v.Boundary || saved.ReplyPath != v.ReplyPath {
		return SocketNotSent, fmt.Errorf("missing immutable Claude claim")
	}
	if !time.Now().Before(saved.Deadline) {
		return SocketNotSent, fmt.Errorf("Claude deadline expired")
	}
	var wait <-chan socketmsg.Message
	cancel := func() {}
	if v.ReplyPath != "" {
		if s.receipts == nil {
			return SocketNotSent, fmt.Errorf("missing receipt channel")
		}
		actual, e := s.receipts.Addr(filepath.Dir(v.Target.SocketPath))
		if e != nil || actual != v.ReplyPath {
			return SocketNotSent, fmt.Errorf("receipt address differs from durable snapshot")
		}
		wait, cancel = s.receipts.Watch(v.WireID)
	}
	defer cancel()
	sess, e := s.st.GetSession(v.Target.RocketSessionID)
	if e != nil {
		return SocketNotSent, e
	}
	result := s.Resolve(ctx, sess)
	if result.State != claudeinstance.Verified || !claudeinstance.SameIdentity(result.Identity, v.Target) {
		return SocketNotSent, ErrNoSocket
	}
	m, e := s.st.GetMessage(v.MessageID)
	if e != nil || (m.Status != "delivering" && m.Status != "accepted") || !time.Now().Before(saved.Deadline) {
		return SocketNotSent, fmt.Errorf("Claude claim no longer active")
	}
	timeout := time.Until(saved.Deadline)
	if timeout > socketmsg.DefaultTimeout {
		timeout = socketmsg.DefaultTimeout
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 || ctx.Err() != nil {
		return SocketNotSent, context.Canceled
	}
	_, outcome, e := socketmsg.SendWithOutcome(v.Target.SocketPath, v.Envelope, socketmsg.Options{From: v.ReplyPath, MsgID: v.WireID, Raw: true, SessionID: v.Target.ClaudeSessionID, Priority: socketmsg.PriorityNext, Timeout: timeout})
	if outcome == socketmsg.NotSent {
		return SocketNotSent, e
	}
	if e != nil || wait == nil {
		return SocketUnknown, e
	}
	window := s.heldWait
	if window <= 0 {
		window = defaultHeldWait
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case receipt := <-wait:
		return s.receiptOutcome(ctx, v, receipt), nil
	case <-timer.C:
		return SocketUnknown, nil
	case <-ctx.Done():
		return SocketUnknown, ctx.Err()
	}
}
func (s *ClaudeSocketSender) receiptOutcome(ctx context.Context, v claudeinstance.Snapshot, m socketmsg.Message) SocketOutcome {
	if m.MsgV != 1 || m.Type != "control" || m.Action != "peer_message_status" || m.OrigMsgID != v.WireID || m.SessionID == "" || m.SessionID != v.Target.ClaudeSessionID || m.From != socketmsg.EncodeAddr(v.Target.SocketPath) {
		return SocketUnknown
	}
	sess, e := s.st.GetSession(v.Target.RocketSessionID)
	if e != nil {
		return SocketUnknown
	}
	r := s.Resolve(ctx, sess)
	if r.State != claudeinstance.Verified || !claudeinstance.SameIdentity(r.Identity, v.Target) {
		return SocketUnknown
	}
	switch m.Status {
	case "held":
		return SocketHeld
	case "delivered":
		return SocketAccepted
	case "denied":
		return SocketDenied
	case "expired":
		return SocketExpired
	default:
		return SocketUnknown
	}
}
func (s *ClaudeSocketSender) handleReceipt(m socketmsg.Message) {
	if s.st == nil {
		return
	}
	v, e := s.st.GetClaudeDeliveryByWireID(m.OrigMsgID)
	if e != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	phase := s.receiptOutcome(ctx, v, m)
	if phase == SocketUnknown {
		return
	}
	_, _ = s.st.SetClaudePhase(v.MessageID, v.WireID, v.Target, string(phase), time.Now())
}
