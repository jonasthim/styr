package events

import (
	"context"
	"testing"
	"time"
)

func TestBusTwoSubscribersBothReceive(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c1, unsub1 := b.Subscribe(ctx, 4)
	defer unsub1()
	c2, unsub2 := b.Subscribe(ctx, 4)
	defer unsub2()

	m := Message{Kind: "session.event", SessionID: "s1"}
	b.Publish(m)

	select {
	case got := <-c1:
		if got.SessionID != "s1" {
			t.Fatalf("c1 got %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("c1 did not receive message")
	}

	select {
	case got := <-c2:
		if got.SessionID != "s1" {
			t.Fatalf("c2 got %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("c2 did not receive message")
	}
}

func TestBusSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Buffer of 1, never drained: the second publish must not block.
	_, unsub := b.Subscribe(ctx, 1)
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(Message{Kind: "session.event", SessionID: "s1"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	if got := b.Dropped(); got == 0 {
		t.Fatalf("Dropped() = %d, want > 0", got)
	}
}

func TestBusUnsubscribeStopsDelivery(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, unsub := b.Subscribe(ctx, 4)
	unsub()

	// Channel must be closed by unsubscribe.
	select {
	case _, ok := <-c:
		if ok {
			t.Fatal("expected channel to be closed with no value")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed by unsubscribe")
	}

	// Publishing after unsubscribe must not panic and must not deliver.
	b.Publish(Message{Kind: "session.event", SessionID: "s1"})
}

func TestBusCtxCancelUnsubscribes(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(context.Background())

	c, unsub := b.Subscribe(ctx, 4)
	defer unsub()

	cancel()

	select {
	case _, ok := <-c:
		if ok {
			t.Fatal("expected channel to be closed with no value")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed after ctx cancel")
	}
}
