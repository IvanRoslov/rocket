package bus

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Bus is an append-only event bus that persists to store and fans out to subscribers.
type Bus struct {
	st *store.Store

	mu          sync.RWMutex
	subscribers map[int64]chan store.Event
	nextID      atomic.Int64
}

// New creates a new Bus backed by the given store.
func New(st *store.Store) *Bus {
	return &Bus{
		st:          st,
		subscribers: make(map[int64]chan store.Event),
	}
}

// Publish appends an event to the store and fans it out to all subscribers.
// If the store fails, the error is logged and fan-out is skipped.
// Slow subscribers (those whose channel buffer is full) lose events.
// Publish never blocks.
func (b *Bus) Publish(typ, sessionID string, data map[string]any) {
	e := store.Event{
		Type:      typ,
		SessionID: sessionID,
		Data:      data,
	}

	// Append to store (sets TS if zero and returns the ID)
	id, err := b.st.AppendEvent(e)
	if err != nil {
		slog.Default().Error("failed to append event to store", "error", err)
		return // skip fan-out if store fails
	}

	// Populate ID and TS from store
	e.ID = id
	// TS was set by AppendEvent if it was 0
	if e.TS == 0 {
		// This shouldn't happen if AppendEvent works correctly,
		// but just in case, we set it here
		e.TS = store.Event{}.TS
	}
	// Re-read the event from store to get the exact TS that was stored
	events, err := b.st.ListEvents(id-1, 1, "")
	if err != nil || len(events) == 0 {
		slog.Default().Error("failed to retrieve published event from store", "error", err)
		return
	}
	e = events[0]

	// Fan out to subscribers with non-blocking send
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers {
		select {
		case ch <- e:
			// Event sent
		default:
			// Channel buffer is full, drop the event for this subscriber
		}
	}
}

// Subscribe returns a receive-only channel and a cancel function.
// The channel is buffered with capacity 64.
// The cancel function unsubscribes and closes the channel; it is idempotent.
func (b *Bus) Subscribe() (ch <-chan store.Event, cancel func()) {
	eventCh := make(chan store.Event, 64)
	id := b.nextID.Add(1)

	b.mu.Lock()
	b.subscribers[id] = eventCh
	b.mu.Unlock()

	var once sync.Once
	cancel = func() {
		once.Do(func() {
			b.mu.Lock()
			if ch, exists := b.subscribers[id]; exists {
				delete(b.subscribers, id)
				close(ch)
			}
			b.mu.Unlock()
		})
	}

	return eventCh, cancel
}
