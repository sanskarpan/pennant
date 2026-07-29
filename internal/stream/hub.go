package stream

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"pennant/internal/events"
)

// Metrics tracks SSE hub operational metrics.
type Metrics struct {
	ActiveConnections     atomic.Int64
	SlowClientDisconnects atomic.Int64
	PublishCount          atomic.Int64
}

// Hub fans out messages to all subscribers of an environment.
type Hub struct {
	mu      sync.RWMutex
	byEnv   map[string]map[string]*Subscriber // envKey -> subID -> sub
	rings   map[string]*EventRing             // envKey -> replay buffer
	bus     *events.Bus
	metrics *Metrics
	subSeq  atomic.Int64
}

func NewHub(bus *events.Bus) *Hub {
	return &Hub{
		byEnv:   make(map[string]map[string]*Subscriber),
		rings:   make(map[string]*EventRing),
		bus:     bus,
		metrics: &Metrics{},
	}
}

func (h *Hub) ensureEnv(envKey string) {
	if h.byEnv[envKey] == nil {
		h.byEnv[envKey] = make(map[string]*Subscriber)
	}
	if h.rings[envKey] == nil {
		h.rings[envKey] = NewEventRing(256)
	}
}

// Subscribe registers a new SSE client.
func (h *Hub) Subscribe(envKey, projectKey, sdkKey, userAgent string) *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ensureEnv(envKey)

	id := fmt.Sprintf("sub-%d", h.subSeq.Add(1))
	sub := &Subscriber{
		ID:          id,
		EnvKey:      envKey,
		ProjectKey:  projectKey,
		SDKKey:      sdkKey,
		Ch:          make(chan Message, 32),
		ConnectedAt: time.Now(),
		UserAgent:   userAgent,
	}
	h.byEnv[envKey][id] = sub
	h.metrics.ActiveConnections.Add(1)
	return sub
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(envKey, subID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byEnv[envKey] != nil {
		if _, ok := h.byEnv[envKey][subID]; ok {
			delete(h.byEnv[envKey], subID)
			h.metrics.ActiveConnections.Add(-1)
		}
	}
}

// Publish fans out to all subscribers for an environment.
// MUST be non-blocking: a slow client must never stall the publisher.
func (h *Hub) Publish(envKey string, msg Message) {
	// Append to replay ring first
	h.mu.RLock()
	if h.rings[envKey] != nil {
		h.rings[envKey].Append(msg)
	}
	subs := make([]*Subscriber, 0, len(h.byEnv[envKey]))
	for _, s := range h.byEnv[envKey] {
		subs = append(subs, s)
	}
	h.mu.RUnlock()

	h.metrics.PublishCount.Add(1)

	var slow []string
	for _, s := range subs {
		select {
		case s.Ch <- msg:
			atomic.StoreInt64(&s.LastSent, msg.Version)
		default:
			// Buffer full. Disconnecting is strictly better than blocking:
			// the client will reconnect and receive a fresh full snapshot.
			slow = append(slow, s.ID)
		}
	}
	for _, id := range slow {
		h.metrics.SlowClientDisconnects.Add(1)
		h.Unsubscribe(envKey, id)
	}
}

// Replay returns messages since the given version, or false if a full snapshot is needed.
func (h *Hub) Replay(envKey string, version int64) ([]Message, bool) {
	h.mu.RLock()
	ring := h.rings[envKey]
	h.mu.RUnlock()
	if ring == nil {
		return nil, false
	}
	return ring.Since(version)
}

// ActiveCount returns the number of active subscribers for an environment.
func (h *Hub) ActiveCount(envKey string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byEnv[envKey])
}

// GetMetrics returns hub metrics.
func (h *Hub) GetMetrics() *Metrics { return h.metrics }
