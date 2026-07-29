package stream

import (
	"testing"
	"time"

	"pennant/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub_NonBlockingPublish(t *testing.T) {
	bus := events.NewBus()
	hub := NewHub(bus)

	// Subscriber that never reads
	slow := hub.Subscribe("env1", "proj", "sdk-key", "test")
	_ = slow

	// Subscriber that reads normally
	fast := hub.Subscribe("env1", "proj", "sdk-key", "test")

	received := make(chan Message, 1100)
	go func() {
		for msg := range fast.Ch {
			received <- msg
		}
	}()

	// Publish 1000 messages — slow client should not block
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hub.Publish("env1", Message{Event: "patch", Version: int64(i + 1)})
		}
		close(done)
	}()

	select {
	case <-done:
		// OK — publish completed without blocking
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked — non-blocking invariant violated")
	}

	// Fast subscriber should have received at least some messages
	time.Sleep(100 * time.Millisecond)
	assert.Greater(t, len(received), 0, "fast subscriber should have received messages")
	t.Logf("slow client disconnects: %d", hub.metrics.SlowClientDisconnects.Load())
}

func TestRing_ReplayWindow(t *testing.T) {
	ring := NewEventRing(10)
	for i := 1; i <= 20; i++ {
		ring.Append(Message{Event: "patch", Version: int64(i)})
	}
	// Version 5 has been evicted (ring only holds 10)
	msgs, ok := ring.Since(5)
	assert.False(t, ok, "version 5 should be evicted from ring of size 10 after 20 messages")
	_ = msgs
}

func TestRing_ReplayDeltas(t *testing.T) {
	ring := NewEventRing(256)
	for i := 1; i <= 100; i++ {
		ring.Append(Message{Event: "patch", Version: int64(i)})
	}

	msgs, ok := ring.Since(90)
	require.True(t, ok, "version 90 should be in ring")
	assert.Equal(t, 10, len(msgs), "should receive versions 91..100")
	for i, msg := range msgs {
		assert.Equal(t, int64(91+i), msg.Version)
	}
}

func TestHub_Subscribe_Unsubscribe(t *testing.T) {
	bus := events.NewBus()
	hub := NewHub(bus)

	sub := hub.Subscribe("env1", "proj", "key", "ua")
	assert.Equal(t, 1, hub.ActiveCount("env1"))

	hub.Unsubscribe("env1", sub.ID)
	assert.Equal(t, 0, hub.ActiveCount("env1"))
}
