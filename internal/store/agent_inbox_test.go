package store

import (
	"errors"
	"testing"
)

func openStoreWithAgent(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	addAgentFixtures(t, s)
	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	return s
}

func TestAddInboxMessageDefaults(t *testing.T) {
	s := openStoreWithAgent(t)

	id, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "deploy is stuck"})
	if err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}
	if id == 0 {
		t.Fatalf("id = 0, want a rowid")
	}

	msgs, err := s.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Status != InboxUnread {
		t.Errorf("Status = %q, want unread", m.Status)
	}
	if m.From != "" {
		t.Errorf("From = %q, want empty (human)", m.From)
	}
	if m.CreatedAt == 0 {
		t.Errorf("CreatedAt not set: %+v", m)
	}
	if m.ReadAt != 0 {
		t.Errorf("ReadAt = %d, want 0", m.ReadAt)
	}
}

func TestAddInboxMessageUnknownAgent(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.AddInboxMessage(InboxMessage{AgentID: "ghost", Body: "hi"}); err == nil {
		t.Fatalf("AddInboxMessage for unknown agent: want foreign key error, got nil")
	}
}

func TestNextUnreadInboxMessageDrainsOldestFirst(t *testing.T) {
	s := openStoreWithAgent(t)

	first, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", From: "task-1-orch", Body: "one"})
	if err != nil {
		t.Fatalf("add first: %v", err)
	}
	second, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "two"})
	if err != nil {
		t.Fatalf("add second: %v", err)
	}

	m, ok, err := s.NextUnreadInboxMessage("sre")
	if err != nil || !ok {
		t.Fatalf("NextUnreadInboxMessage = %v, ok=%v", err, ok)
	}
	if m.ID != first || m.Body != "one" || m.From != "task-1-orch" {
		t.Fatalf("first next = %+v", m)
	}
	if m.Status != InboxRead || m.ReadAt == 0 {
		t.Errorf("first next not marked read: %+v", m)
	}

	n, err := s.CountUnreadInbox("sre")
	if err != nil {
		t.Fatalf("CountUnreadInbox: %v", err)
	}
	if n != 1 {
		t.Errorf("unread = %d, want 1", n)
	}

	m, ok, err = s.NextUnreadInboxMessage("sre")
	if err != nil || !ok || m.ID != second {
		t.Fatalf("second next = %+v, ok=%v, err=%v", m, ok, err)
	}

	if _, ok, err = s.NextUnreadInboxMessage("sre"); err != nil || ok {
		t.Fatalf("third next: ok=%v, err=%v, want ok=false", ok, err)
	}
}

func TestNextUnreadInboxMessageEmpty(t *testing.T) {
	s := openStoreWithAgent(t)
	if _, ok, err := s.NextUnreadInboxMessage("sre"); err != nil || ok {
		t.Fatalf("NextUnreadInboxMessage on empty inbox: ok=%v, err=%v", ok, err)
	}
}

func TestGetInboxMessageDoesNotMarkRead(t *testing.T) {
	s := openStoreWithAgent(t)

	id, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "peek me"})
	if err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}

	m, err := s.GetInboxMessage(id)
	if err != nil {
		t.Fatalf("GetInboxMessage: %v", err)
	}
	if m.Body != "peek me" || m.Status != InboxUnread {
		t.Fatalf("message = %+v", m)
	}

	n, err := s.CountUnreadInbox("sre")
	if err != nil {
		t.Fatalf("CountUnreadInbox: %v", err)
	}
	if n != 1 {
		t.Errorf("unread after peek = %d, want 1", n)
	}
}

func TestGetInboxMessageNotFound(t *testing.T) {
	s := openStoreWithAgent(t)
	if _, err := s.GetInboxMessage(404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetInboxMessage = %v, want ErrNotFound", err)
	}
}

func TestListInboxMessagesFiltersAndLimits(t *testing.T) {
	s := openStoreWithAgent(t)
	for i := 0; i < 3; i++ {
		if _, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "m"}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if _, _, err := s.NextUnreadInboxMessage("sre"); err != nil {
		t.Fatalf("next: %v", err)
	}

	unread, err := s.ListInboxMessages("sre", InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages(unread): %v", err)
	}
	if len(unread) != 2 {
		t.Fatalf("unread = %d, want 2", len(unread))
	}

	read, err := s.ListInboxMessages("sre", InboxRead, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages(read): %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("read = %d, want 1", len(read))
	}

	limited, err := s.ListInboxMessages("sre", "", 2)
	if err != nil {
		t.Fatalf("ListInboxMessages(limit): %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited = %d, want 2", len(limited))
	}
}

func TestMaxUnreadInboxID(t *testing.T) {
	s := openStoreWithAgent(t)

	got, err := s.MaxUnreadInboxID("sre")
	if err != nil {
		t.Fatalf("MaxUnreadInboxID(empty): %v", err)
	}
	if got != 0 {
		t.Fatalf("MaxUnreadInboxID(empty) = %d, want 0", got)
	}

	if _, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "one"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	last, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "two"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err = s.MaxUnreadInboxID("sre")
	if err != nil {
		t.Fatalf("MaxUnreadInboxID: %v", err)
	}
	if got != last {
		t.Fatalf("MaxUnreadInboxID = %d, want %d", got, last)
	}

	// Draining everything takes it back to zero.
	for {
		_, ok, err := s.NextUnreadInboxMessage("sre")
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if !ok {
			break
		}
	}
	got, err = s.MaxUnreadInboxID("sre")
	if err != nil {
		t.Fatalf("MaxUnreadInboxID after drain: %v", err)
	}
	if got != 0 {
		t.Fatalf("MaxUnreadInboxID after drain = %d, want 0", got)
	}
}
