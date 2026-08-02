package api

import (
	"errors"
	"log/slog"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// deliverToAgent is the single path every message addressed to an agent takes,
// whatever it came from — `rocket send <agent>`, the dashboard's message box,
// or a human entry in a Q&A thread.
//
// The agent's tmux session is named after the agent, so liveness is a session
// lookup: alive means the message goes through the normal delivery queue and
// lands in the agent's terminal like any other; not alive means it becomes an
// unread inbox row the agent pulls itself with `rocket inbox next`.
//
// live reports which of the two happened, so callers can tell the sender.
func deliverToAgent(d Deps, agentID, from, body string) (live bool, msgID int64, err error) {
	body = rewriteAttachmentLinks(d, body)

	sess, err := d.Store.GetSession(agentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No session row at all: the agent has never been started.
	case err != nil:
		return false, 0, err
	case sess.Kind != session.AgentSessionKind:
		// An id collision with a real session: never inject into it.
		return false, 0, errors.New("session " + agentID + " is not an agent session")
	case sess.State == "spawning" || sess.State == "running":
		id, err := d.Store.AddMessage(store.Message{
			FromSession: senderIfSession(d, from),
			ToSession:   agentID,
			Body:        deliveryBody(d, from, body),
		})
		if err != nil {
			return false, 0, err
		}
		if d.Bus != nil {
			d.Bus.Publish("message.queued", agentID, map[string]any{
				"id": id, "from": from, "to": agentID,
			})
		}
		if d.Queue != nil {
			d.Queue.Wake(agentID)
		} else {
			slog.Warn("api: message to agent queued with nil Queue", "id", id, "to", agentID)
		}
		return true, id, nil
	}

	inboxID, err := d.Store.AddInboxMessage(store.InboxMessage{
		AgentID: agentID,
		From:    from,
		Body:    body,
	})
	if err != nil {
		return false, 0, err
	}
	return false, inboxID, nil
}

// deliverToSession enqueues body to an ephemeral session — an orchestrator or
// a worker taking part in a thread. It is the generalisation of the old
// deliverToOrchestrator: the same enqueue pattern POST /v1/messages uses
// (insert queued, publish message.queued, wake the delivery worker), with the
// recipient named directly instead of derived from a task.
//
// Unlike an agent, an ephemeral session has no inbox: if it is gone or
// terminal the message is dropped with a log, because there is nothing to
// deliver it to later. The thread record itself is written regardless.
func deliverToSession(d Deps, sessionID, body string) error {
	if sessionID == "" {
		return nil
	}
	body = rewriteAttachmentLinks(d, body)

	sess, err := d.Store.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.Warn("api: thread delivery target session not found, skipping", "session_id", sessionID)
			return nil
		}
		return err
	}
	if isSessionTerminal(sess.State) {
		slog.Warn("api: thread delivery target session is terminal, skipping",
			"session_id", sessionID, "state", sess.State)
		return nil
	}

	id, err := d.Store.AddMessage(store.Message{ToSession: sessionID, Body: body})
	if err != nil {
		return err
	}

	if d.Bus != nil {
		d.Bus.Publish("message.queued", sessionID, map[string]any{
			"id": id, "from": "", "to": sessionID,
		})
	}
	if d.Queue != nil {
		d.Queue.Wake(sessionID)
	} else {
		slog.Warn("api: thread message queued with nil Queue, will not be delivered until daemon restart",
			"id", id, "to", sessionID)
	}
	return nil
}

// senderIfSession returns from only when it names a session the store knows:
// the queue looks senders up to report delivery failures back to them, so an
// unknown sender must not be recorded as one.
func senderIfSession(d Deps, from string) string {
	if from == "" {
		return ""
	}
	if _, err := d.Store.GetSession(from); err != nil {
		return ""
	}
	return from
}

// deliveryBody prefixes the body with the sender when the sender cannot be
// recorded on the message itself (see senderIfSession) — the agent should
// still see who wrote to it.
func deliveryBody(d Deps, from, body string) string {
	if from == "" || senderIfSession(d, from) != "" {
		return body
	}
	return "[from " + from + "] " + body
}
