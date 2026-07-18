package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestPublishWritesToStoreAndDeliversToSubscribers(t *testing.T) {
	// Setup: create a real store on a temp directory
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	b := New(st)

	// Subscribe two subscribers
	ch1, cancel1 := b.Subscribe()
	ch2, cancel2 := b.Subscribe()
	defer cancel1()
	defer cancel2()

	// Publish an event
	eventType := "user.created"
	sessionID := "session-123"
	data := map[string]any{"user_id": 456}

	b.Publish(eventType, sessionID, data)

	// Verify event is delivered to both subscribers
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case event := <-ch1:
		if event.Type != eventType || event.SessionID != sessionID {
			t.Errorf("subscriber 1: got type=%q sessionID=%q, want type=%q sessionID=%q",
				event.Type, event.SessionID, eventType, sessionID)
		}
		if id, ok := event.Data["user_id"].(float64); !ok || int(id) != 456 {
			t.Errorf("subscriber 1: got data=%v, want user_id=456", event.Data)
		}
	case <-ctx.Done():
		t.Fatal("subscriber 1: event not received")
	}

	select {
	case event := <-ch2:
		if event.Type != eventType || event.SessionID != sessionID {
			t.Errorf("subscriber 2: got type=%q sessionID=%q, want type=%q sessionID=%q",
				event.Type, event.SessionID, eventType, sessionID)
		}
	case <-ctx.Done():
		t.Fatal("subscriber 2: event not received")
	}

	// Verify event was stored
	events, err := st.ListEvents(0, -1, "")
	if err != nil {
		t.Fatalf("failed to list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event in store, got %d", len(events))
	}
	if events[0].Type != eventType {
		t.Errorf("stored event: got type=%q, want type=%q", events[0].Type, eventType)
	}
}

func TestAfterCancelNoDelivery(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	b := New(st)

	// Subscribe and immediately cancel
	ch, cancel := b.Subscribe()
	cancel()

	// Publish an event
	b.Publish("test.event", "session-1", map[string]any{})

	// Verify no event is received (channel should be closed)
	ctx, ctxCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer ctxCancel()

	select {
	case event, ok := <-ch:
		if ok {
			t.Errorf("received event after cancel: %v", event)
		}
		// Channel is closed, which is expected
	case <-ctx.Done():
		// Timeout is acceptable - no event should come through
	}
}

func TestDoubleCancelSafe(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	b := New(st)
	_, cancel := b.Subscribe()

	// Call cancel twice - should not panic
	cancel()
	cancel() // Should be safe
}

func TestBufferOverflowDoesNotBlockPublish(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	b := New(st)

	// Subscribe a slow subscriber (we won't read from it)
	ch, cancel := b.Subscribe()
	defer cancel()

	// Publish more events than the buffer size (64)
	numEvents := 80
	numPublished := atomic.Int32{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numEvents; i++ {
			b.Publish("test.event", "", map[string]any{"seq": i})
			numPublished.Add(1)
		}
	}()

	// Give the goroutine time to publish
	wg.Wait()

	// Verify all events were published without blocking
	if numPublished.Load() != int32(numEvents) {
		t.Errorf("not all events published: got %d, want %d", numPublished.Load(), numEvents)
	}

	// Read some events from the channel to verify they were delivered
	received := 0
	ctx, ctxCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer ctxCancel()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed
			}
			received++
		case <-ctx.Done():
			if received == 0 {
				t.Error("received 0 events from channel")
			}
			return
		}
	}
}

func TestPublishSetsTimestamp(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	b := New(st)
	ch, cancel := b.Subscribe()
	defer cancel()

	before := time.Now().Unix()
	b.Publish("test.event", "session-1", nil)
	after := time.Now().Unix()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer ctxCancel()

	select {
	case event := <-ch:
		if event.TS < before || event.TS > after {
			t.Errorf("event timestamp %d not in range [%d, %d]", event.TS, before, after)
		}
	case <-ctx.Done():
		t.Fatal("event not received")
	}
}
