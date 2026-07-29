package stream

import "sync"

// EventRing keeps the last N deltas per environment for Last-Event-ID replay.
type EventRing struct {
	mu       sync.RWMutex
	buf      []Message
	capacity int
	head     int // next write position
	count    int // number of entries
	minVer   int64
	maxVer   int64
}

func NewEventRing(capacity int) *EventRing {
	return &EventRing{
		buf:      make([]Message, capacity),
		capacity: capacity,
	}
}

// Append adds a message to the ring. Overwrites oldest on overflow.
func (r *EventRing) Append(msg Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = msg
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	if msg.Version > r.maxVer {
		r.maxVer = msg.Version
	}
	// Update minVer by scanning
	if r.count == r.capacity {
		r.minVer = r.buf[r.head].Version // oldest is the one we just overwrote
	} else if r.count == 1 {
		r.minVer = msg.Version
	}
}

// Since returns all messages after the given version.
// Returns ok=false if the version is too old (evicted from the ring).
func (r *EventRing) Since(version int64) ([]Message, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.count == 0 {
		return nil, true
	}
	if version < r.minVer-1 {
		return nil, false // too old, full snapshot required
	}
	if version >= r.maxVer {
		return nil, true // already current
	}
	var out []Message
	// Walk in order from oldest to newest
	start := (r.head - r.count + r.capacity) % r.capacity
	for i := 0; i < r.count; i++ {
		msg := r.buf[(start+i)%r.capacity]
		if msg.Version > version {
			out = append(out, msg)
		}
	}
	return out, true
}
