package store

import (
	"errors"
	"testing"
)

func TestAddMessage_DefaultsStatusAndCreatedAt(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddMessage(Message{ToSession: "sess-a", Body: "hi"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	m, err := s.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Status != "queued" {
		t.Errorf("Status = %q, want queued", m.Status)
	}
	if m.CreatedAt == 0 {
		t.Error("CreatedAt not defaulted")
	}
	if m.FromSession != "" {
		t.Errorf("FromSession = %q, want empty", m.FromSession)
	}
	if m.DeliveredAt != 0 {
		t.Errorf("DeliveredAt = %d, want 0", m.DeliveredAt)
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	s := openTestStore(t)

	_, err := s.GetMessage(999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListMessages_BothSidesAscendingLimited(t *testing.T) {
	s := openTestStore(t)

	// a -> b, b -> a, c -> b (unrelated to "a")
	id1, _ := s.AddMessage(Message{FromSession: "a", ToSession: "b", Body: "m1"})
	id2, _ := s.AddMessage(Message{FromSession: "b", ToSession: "a", Body: "m2"})
	_, _ = s.AddMessage(Message{FromSession: "c", ToSession: "b", Body: "m3"})
	id4, _ := s.AddMessage(Message{FromSession: "a", ToSession: "b", Body: "m4"})

	msgs, err := s.ListMessages("a", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	wantIDs := []int64{id1, id2, id4}
	for i, want := range wantIDs {
		if msgs[i].ID != want {
			t.Errorf("msgs[%d].ID = %d, want %d", i, msgs[i].ID, want)
		}
	}

	// limit applies to the most recent N, still ascending
	limited, err := s.ListMessages("a", 2)
	if err != nil {
		t.Fatalf("ListMessages limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("len = %d, want 2", len(limited))
	}
	if limited[0].ID != id2 || limited[1].ID != id4 {
		t.Errorf("limited IDs = %d,%d, want %d,%d", limited[0].ID, limited[1].ID, id2, id4)
	}
}

func TestNextQueuedMessage_FIFOAndNone(t *testing.T) {
	s := openTestStore(t)

	_, ok, err := s.NextQueuedMessage("recipient")
	if err != nil {
		t.Fatalf("NextQueuedMessage: %v", err)
	}
	if ok {
		t.Fatal("expected no queued message")
	}

	id1, _ := s.AddMessage(Message{ToSession: "recipient", Body: "first"})
	id2, _ := s.AddMessage(Message{ToSession: "recipient", Body: "second"})

	m, ok, err := s.NextQueuedMessage("recipient")
	if err != nil || !ok {
		t.Fatalf("NextQueuedMessage: ok=%v err=%v", ok, err)
	}
	if m.ID != id1 {
		t.Errorf("ID = %d, want %d (FIFO)", m.ID, id1)
	}

	if err := s.UpdateMessageStatus(id1, "delivered", 1, 123); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	m2, ok, err := s.NextQueuedMessage("recipient")
	if err != nil || !ok {
		t.Fatalf("NextQueuedMessage after first delivered: ok=%v err=%v", ok, err)
	}
	if m2.ID != id2 {
		t.Errorf("ID = %d, want %d", m2.ID, id2)
	}
}

func TestUpdateMessageStatus_NotFound(t *testing.T) {
	s := openTestStore(t)

	err := s.UpdateMessageStatus(999, "delivered", 1, 100)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateMessageStatus_DeliveredAtZeroIsNull(t *testing.T) {
	s := openTestStore(t)

	id, _ := s.AddMessage(Message{ToSession: "b", Body: "hi"})
	if err := s.UpdateMessageStatus(id, "delivering", 1, 0); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	m, err := s.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.DeliveredAt != 0 {
		t.Errorf("DeliveredAt = %d, want 0", m.DeliveredAt)
	}
	if m.Status != "delivering" || m.Attempts != 1 {
		t.Errorf("Status/Attempts = %q/%d, want delivering/1", m.Status, m.Attempts)
	}
}

func TestExpireQueuedBefore(t *testing.T) {
	s := openTestStore(t)

	old, _ := s.AddMessage(Message{ToSession: "b", Body: "old", CreatedAt: 100})
	recent, _ := s.AddMessage(Message{ToSession: "b", Body: "recent", CreatedAt: 1000})
	delivered, _ := s.AddMessage(Message{ToSession: "b", Body: "delivered", CreatedAt: 50})
	if err := s.UpdateMessageStatus(delivered, "delivered", 1, 60); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	expired, err := s.ExpireQueuedBefore(500)
	if err != nil {
		t.Fatalf("ExpireQueuedBefore: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != old {
		t.Fatalf("expired = %+v, want just %d", expired, old)
	}
	if expired[0].Status != "failed" {
		t.Errorf("expired status = %q, want failed", expired[0].Status)
	}

	m, err := s.GetMessage(old)
	if err != nil {
		t.Fatalf("GetMessage(old): %v", err)
	}
	if m.Status != "failed" {
		t.Errorf("persisted status = %q, want failed", m.Status)
	}

	m2, err := s.GetMessage(recent)
	if err != nil {
		t.Fatalf("GetMessage(recent): %v", err)
	}
	if m2.Status != "queued" {
		t.Errorf("recent status = %q, want queued (untouched)", m2.Status)
	}

	// Delivered message must remain untouched.
	m3, err := s.GetMessage(delivered)
	if err != nil {
		t.Fatalf("GetMessage(delivered): %v", err)
	}
	if m3.Status != "delivered" {
		t.Errorf("delivered status = %q, want delivered", m3.Status)
	}

	// No expiry when nothing matches.
	none, err := s.ExpireQueuedBefore(1)
	if err != nil {
		t.Fatalf("ExpireQueuedBefore(none): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("none = %+v, want empty", none)
	}
}

func TestListQueuedRecipients(t *testing.T) {
	s := openTestStore(t)

	recips, err := s.ListQueuedRecipients()
	if err != nil {
		t.Fatalf("ListQueuedRecipients: %v", err)
	}
	if len(recips) != 0 {
		t.Fatalf("recips = %v, want empty", recips)
	}

	id1, _ := s.AddMessage(Message{ToSession: "a", Body: "1"})
	_, _ = s.AddMessage(Message{ToSession: "a", Body: "2"})
	_, _ = s.AddMessage(Message{ToSession: "b", Body: "3"})
	delivered, _ := s.AddMessage(Message{ToSession: "c", Body: "4"})
	if err := s.UpdateMessageStatus(delivered, "delivered", 1, 10); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	recips, err = s.ListQueuedRecipients()
	if err != nil {
		t.Fatalf("ListQueuedRecipients: %v", err)
	}
	got := map[string]bool{}
	for _, r := range recips {
		got[r] = true
	}
	if len(got) != 2 || !got["a"] || !got["b"] {
		t.Fatalf("recips = %v, want [a b]", recips)
	}

	if err := s.UpdateMessageStatus(id1, "failed", 1, 0); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}
	recips, err = s.ListQueuedRecipients()
	if err != nil {
		t.Fatalf("ListQueuedRecipients: %v", err)
	}
	got = map[string]bool{}
	for _, r := range recips {
		got[r] = true
	}
	if len(got) != 2 {
		t.Fatalf("recips = %v, want still [a b] (a still has one queued)", recips)
	}
}

func TestExpireQueuedBefore_ConcurrentDeliveringNotExpired(t *testing.T) {
	s := openTestStore(t)

	queuedExpired, _ := s.AddMessage(Message{ToSession: "b", Body: "queued-expired", CreatedAt: 100})
	deliveringExpired, _ := s.AddMessage(Message{ToSession: "b", Body: "delivering-expired", CreatedAt: 100})
	if err := s.UpdateMessageStatus(deliveringExpired, "delivering", 1, 0); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	expired, err := s.ExpireQueuedBefore(500)
	if err != nil {
		t.Fatalf("ExpireQueuedBefore: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != queuedExpired {
		t.Fatalf("expired = %+v, want just %d (queued one)", expired, queuedExpired)
	}

	m, err := s.GetMessage(deliveringExpired)
	if err != nil {
		t.Fatalf("GetMessage(deliveringExpired): %v", err)
	}
	if m.Status != "delivering" {
		t.Errorf("delivering message status = %q, want unchanged delivering", m.Status)
	}
}

func TestResetDelivering(t *testing.T) {
	s := openTestStore(t)

	delivering1, _ := s.AddMessage(Message{ToSession: "a", Body: "1"})
	if err := s.UpdateMessageStatus(delivering1, "delivering", 1, 0); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}
	delivering2, _ := s.AddMessage(Message{ToSession: "b", Body: "2"})
	if err := s.UpdateMessageStatus(delivering2, "delivering", 2, 0); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}
	queued, _ := s.AddMessage(Message{ToSession: "c", Body: "3"})
	delivered, _ := s.AddMessage(Message{ToSession: "d", Body: "4"})
	if err := s.UpdateMessageStatus(delivered, "delivered", 1, 10); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	n, err := s.ResetDelivering()
	if err != nil {
		t.Fatalf("ResetDelivering: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}

	for _, id := range []int64{delivering1, delivering2} {
		m, err := s.GetMessage(id)
		if err != nil {
			t.Fatalf("GetMessage(%d): %v", id, err)
		}
		if m.Status != "queued" {
			t.Errorf("message %d status = %q, want queued", id, m.Status)
		}
	}

	// Untouched statuses remain untouched.
	if m, _ := s.GetMessage(queued); m.Status != "queued" {
		t.Errorf("queued message status = %q, want queued", m.Status)
	}
	if m, _ := s.GetMessage(delivered); m.Status != "delivered" {
		t.Errorf("delivered message status = %q, want delivered", m.Status)
	}

	// Idempotent: nothing left to reset.
	n2, err := s.ResetDelivering()
	if err != nil {
		t.Fatalf("ResetDelivering (second call): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("n2 = %d, want 0", n2)
	}
}

func TestPurgeOld(t *testing.T) {
	s := openTestStore(t)

	oldMsg, _ := s.AddMessage(Message{ToSession: "a", Body: "old", CreatedAt: 100})
	newMsg, _ := s.AddMessage(Message{ToSession: "a", Body: "new", CreatedAt: 1000})

	oldEvt, err := s.AppendEvent(Event{TS: 100, Type: "test", Data: map[string]any{}})
	if err != nil {
		t.Fatalf("AppendEvent old: %v", err)
	}
	newEvt, err := s.AppendEvent(Event{TS: 1000, Type: "test", Data: map[string]any{}})
	if err != nil {
		t.Fatalf("AppendEvent new: %v", err)
	}

	if err := s.PurgeOld(500); err != nil {
		t.Fatalf("PurgeOld: %v", err)
	}

	if _, err := s.GetMessage(oldMsg); !errors.Is(err, ErrNotFound) {
		t.Errorf("old message not purged: err=%v", err)
	}
	if _, err := s.GetMessage(newMsg); err != nil {
		t.Errorf("new message wrongly purged: %v", err)
	}

	evts, err := s.ListEvents(0, 0, "")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	ids := map[int64]bool{}
	for _, e := range evts {
		ids[e.ID] = true
	}
	if ids[oldEvt] {
		t.Error("old event not purged")
	}
	if !ids[newEvt] {
		t.Error("new event wrongly purged")
	}
}
