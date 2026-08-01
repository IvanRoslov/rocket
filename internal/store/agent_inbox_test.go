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

func TestEnqueueInboxEventDefaults(t *testing.T) {
	s := openStoreWithAgent(t)

	id, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "sre", Kind: "message"})
	if err != nil {
		t.Fatalf("EnqueueInboxEvent: %v", err)
	}
	if id == 0 {
		t.Fatalf("id = 0, want a rowid")
	}

	events, err := s.ListInboxEvents("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Status != InboxStatusQueued {
		t.Errorf("Status = %q, want queued", e.Status)
	}
	if e.Payload != "{}" {
		t.Errorf("Payload = %q, want {}", e.Payload)
	}
	if e.CreatedAt == 0 || e.UpdatedAt == 0 {
		t.Errorf("timestamps not set: %+v", e)
	}
}

func TestEnqueueInboxEventUnknownRole(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "ghost", Kind: "message"}); err == nil {
		t.Fatalf("EnqueueInboxEvent for unknown role: want foreign key error, got nil")
	}
}

func TestQueuedInboxEventsAndMarks(t *testing.T) {
	s := openStoreWithAgent(t)

	first, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "sre", Kind: "message", Payload: `{"text":"hi"}`})
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "sre", Kind: "cron"})
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}

	queued, err := s.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	if len(queued) != 2 || queued[0].ID != first || queued[1].ID != second {
		t.Fatalf("queued = %+v, want oldest-first [%d %d]", queued, first, second)
	}
	if queued[0].Payload != `{"text":"hi"}` {
		t.Errorf("Payload = %q", queued[0].Payload)
	}

	if err := s.MarkInboxDelivered([]int64{first}); err != nil {
		t.Fatalf("MarkInboxDelivered: %v", err)
	}
	n, err := s.CountQueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("CountQueuedInboxEvents: %v", err)
	}
	if n != 1 {
		t.Errorf("queued count = %d, want 1", n)
	}

	delivered, err := s.ListInboxEvents("sre", InboxStatusDelivered, 0)
	if err != nil {
		t.Fatalf("ListInboxEvents(delivered): %v", err)
	}
	if len(delivered) != 1 || delivered[0].ID != first {
		t.Fatalf("delivered = %+v", delivered)
	}

	if err := s.MarkInboxDone([]int64{first, second}); err != nil {
		t.Fatalf("MarkInboxDone: %v", err)
	}
	done, err := s.ListInboxEvents("sre", InboxStatusDone, 0)
	if err != nil {
		t.Fatalf("ListInboxEvents(done): %v", err)
	}
	if len(done) != 2 {
		t.Fatalf("done = %d, want 2", len(done))
	}

	// Marking an empty set is a no-op, not an error.
	if err := s.MarkInboxDone(nil); err != nil {
		t.Fatalf("MarkInboxDone(nil): %v", err)
	}
}

func TestListInboxEventsLimit(t *testing.T) {
	s := openStoreWithAgent(t)
	for i := 0; i < 3; i++ {
		if _, err := s.EnqueueInboxEvent(AgentInboxEvent{RoleID: "sre", Kind: "message"}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	events, err := s.ListInboxEvents("sre", "", 2)
	if err != nil {
		t.Fatalf("ListInboxEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestUpsertAgentItemInsertThenUpdate(t *testing.T) {
	s := openStoreWithAgent(t)

	created, err := s.UpsertAgentItem(AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#12",
	})
	if err != nil {
		t.Fatalf("UpsertAgentItem insert: %v", err)
	}
	if created.State != "new" {
		t.Errorf("State = %q, want new", created.State)
	}
	if created.TaskID != 0 || created.SnoozeUntil != 0 {
		t.Errorf("nullable fields = %+v, want zero", created)
	}

	updated, err := s.UpsertAgentItem(AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#12",
		State: "deferred", Note: "waiting for db migration", TaskID: 45, SnoozeUntil: 1800000000,
	})
	if err != nil {
		t.Fatalf("UpsertAgentItem update: %v", err)
	}
	if updated.ID != created.ID {
		t.Errorf("ID = %d, want %d (upsert must keep the row)", updated.ID, created.ID)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Errorf("CreatedAt = %d, want %d", updated.CreatedAt, created.CreatedAt)
	}
	if updated.State != "deferred" || updated.Note != "waiting for db migration" ||
		updated.TaskID != 45 || updated.SnoozeUntil != 1800000000 {
		t.Errorf("updated = %+v", updated)
	}
}

func TestListAgentItemsFiltersByState(t *testing.T) {
	s := openStoreWithAgent(t)

	if _, err := s.UpsertAgentItem(AgentItem{RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#1", State: "taken"}); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if _, err := s.UpsertAgentItem(AgentItem{RoleID: "sre", Kind: "task", ExternalRef: "task:45", State: "deferred"}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	all, err := s.ListAgentItems("sre", "")
	if err != nil {
		t.Fatalf("ListAgentItems: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2", len(all))
	}

	deferred, err := s.ListAgentItems("sre", "deferred")
	if err != nil {
		t.Fatalf("ListAgentItems(deferred): %v", err)
	}
	if len(deferred) != 1 || deferred[0].ExternalRef != "task:45" {
		t.Fatalf("deferred = %+v", deferred)
	}
}

func TestGetAgentItemNotFound(t *testing.T) {
	s := openStoreWithAgent(t)
	if _, err := s.GetAgentItem("sre", "issue", "acme/platform#404"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgentItem = %v, want ErrNotFound", err)
	}
}
