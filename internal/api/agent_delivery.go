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
func deliverToAgent(d Deps, agentID, from, body string) (live bool, err error) {
	body = rewriteAttachmentLinks(d, body)

	sess, err := d.Store.GetSession(agentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No session row at all: the agent has never been started.
	case err != nil:
		return false, err
	case sess.Kind != session.AgentSessionKind:
		// An id collision with a real session: never inject into it.
		return false, errors.New("session " + agentID + " is not an agent session")
	case sess.State == "spawning" || sess.State == "running":
		id, err := d.Store.AddMessage(store.Message{
			FromSession: senderIfSession(d, from),
			ToSession:   agentID,
			Body:        deliveryBody(d, from, body),
		})
		if err != nil {
			return false, err
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
		return true, nil
	}

	if _, err := d.Store.AddInboxMessage(store.InboxMessage{
		AgentID: agentID,
		From:    from,
		Body:    body,
	}); err != nil {
		return false, err
	}
	return false, nil
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
