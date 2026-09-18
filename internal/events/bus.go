// Package events is the in-process publish/subscribe bus that feeds the SSE
// endpoint. Publishing never blocks: slow subscribers drop the message and
// the bus counts the drop.
package events

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
)

// Message is one bus event, fanned out to every subscriber.
type Message struct {
	Kind      string          `json:"kind"` // "session.event" | "session.state" | "approval.created" | "approval.decided" | "session.stats"
	SessionID string          `json:"session_id"`
	OwnerID   *string         `json:"owner_id"` // for visibility filtering in the SSE handler
	Seq       int64           `json:"seq,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type subscriber struct {
	ch chan Message
}

// Bus is an in-process publish/subscribe hub. The zero value is not usable;
// construct one with New. A *Bus is safe for concurrent use.
type Bus struct {
	mu      sync.RWMutex
	subs    map[uint64]*subscriber
	next    uint64
	dropped uint64
}

// New returns a ready-to-use Bus.
func New() *Bus {
	return &Bus{subs: make(map[uint64]*subscriber)}
}

// Publish fans m out to every current subscriber. It never blocks: a
// subscriber whose buffer is full has the message dropped, and the drop is
// counted in Dropped().
func (b *Bus) Publish(m Message) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		select {
		case s.ch <- m:
		default:
			atomic.AddUint64(&b.dropped, 1)
		}
	}
}

// Dropped returns the total number of messages dropped across all
// subscribers because their buffer was full.
func (b *Bus) Dropped() uint64 {
	return atomic.LoadUint64(&b.dropped)
}

// Subscribe registers a new subscriber with the given channel buffer size
// and returns the channel to receive on plus a function that unsubscribes
// and closes the channel. The returned function is safe to call more than
// once. Cancelling ctx also unsubscribes.
func (b *Bus) Subscribe(ctx context.Context, buffer int) (<-chan Message, func()) {
	b.mu.Lock()
	b.next++
	id := b.next
	s := &subscriber{ch: make(chan Message, buffer)}
	b.subs[id] = s
	b.mu.Unlock()

	stop := make(chan struct{})
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			close(stop)
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(s.ch)
		})
	}

	go func() {
		select {
		case <-ctx.Done():
			unsubscribe()
		case <-stop:
		}
	}()

	return s.ch, unsubscribe
}
