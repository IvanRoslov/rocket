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
	l, pinned, observed, e := s.st.GetClaudeBinding(ctx, sess.ID)
	if e != nil {
		return rejected("unproven_launch")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	registry, e := claudeinstance.ReadRegistry(ctx, s.sessionsDir)
	if e != nil {
		return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "registry_lookup"}
	}
	resolution := claudeinstance.Resolve(ctx, l, pinned, registry, s.probe, filepath.Join(filepath.Dir(s.sessionsDir), "projects"))
	if resolution.State == claudeinstance.Verified {
		var ok bool
		if pinned != nil && !claudeinstance.SameIdentity(*pinned, resolution.Identity) {
			// Repeat the full physical validation immediately before the transition CAS.
			fresh, err := claudeinstance.ReadRegistry(ctx, s.sessionsDir)
			if err != nil {
				return rejected("registry_lookup")
			}
			again := claudeinstance.Resolve(ctx, l, pinned, fresh, s.probe, filepath.Join(filepath.Dir(s.sessionsDir), "projects"))
			if again.State != claudeinstance.Verified || !claudeinstance.SameIdentity(again.Identity, resolution.Identity) {
				return rejected("binding_changed")
			}
			ok, e = s.st.AdvanceClaudeBinding(ctx, *pinned, resolution.Identity, l.BindingRevision, observed, time.Now().UnixNano())
		} else {
			ok, e = s.st.BindClaudeIdentity(resolution.Identity)
		}
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
	launch, pinned, _, e := s.st.GetClaudeBinding(ctx, sess.ID)
	if e != nil || pinned == nil || !claudeinstance.SameIdentity(*pinned, result.Identity) {
		return claudeinstance.Snapshot{}, ErrNoSocket
	}
	envelope, e := socketmsg.EnvelopeWithID(wire, from, fromName, text)
	if e != nil {
		return claudeinstance.Snapshot{}, e
	}
	return claudeinstance.Snapshot{BindingRevision: launch.BindingRevision, EnvelopeVersion: 1, Transport: ClaudeSocketTransport, Target: result.Identity, WireID: wire, Envelope: envelope, ReplyPath: reply, Boundary: boundary}, nil
}
func (s *ClaudeSocketSender) Send(ctx context.Context, v claudeinstance.Snapshot) (SocketOutcome, error) {
	if s.st == nil {
		return SocketNotSent, ErrNoSocket
	}
	saved, e := s.st.GetClaudeDelivery(v.MessageID)
	if e != nil || saved.BindingRevision != v.BindingRevision || saved.EnvelopeVersion != v.EnvelopeVersion || saved.WireID != v.WireID || !claudeinstance.SameIdentity(saved.Target, v.Target) || saved.Envelope != v.Envelope || saved.Boundary != v.Boundary || saved.ReplyPath != v.ReplyPath {
		return SocketNotSent, fmt.Errorf("missing immutable Claude claim")
	}
	if claudeAgeExpired(saved.Deadline, time.Now()) {
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
	launch, pinned, _, err := s.st.GetClaudeBinding(ctx, v.Target.RocketSessionID)
	current, sessionErr := s.st.GetSession(v.Target.RocketSessionID)
	if err != nil || sessionErr != nil || pinned == nil || launch.BindingRevision != v.BindingRevision || !claudeinstance.SameIdentity(*pinned, v.Target) || !claudeEligible(current.Activity) {
		return SocketNotSent, ErrNoSocket
	}
	m, e := s.st.GetMessage(v.MessageID)
	if e != nil || (m.Status != "delivering" && m.Status != "accepted") || claudeAgeExpired(saved.Deadline, time.Now()) {
		return SocketNotSent, fmt.Errorf("Claude claim no longer active")
	}
	timeout := socketmsg.DefaultTimeout
	if !saved.Deadline.IsZero() && time.Until(saved.Deadline) < timeout {
		timeout = time.Until(saved.Deadline)
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
