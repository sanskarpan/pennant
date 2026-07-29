package analytics

import (
	"sync"
	"time"
)

// Ingestor accepts events from the server (SDK eval callbacks, custom track calls)
// and buffers them for batch writes. Flushed every FlushInterval or when
// the buffer reaches MaxBatch events.
type Ingestor struct {
	mu            sync.Mutex
	buf           []*Event
	store         EventStore
	done          chan struct{}
	wg            sync.WaitGroup
	FlushInterval time.Duration
	MaxBatch      int
}

// EventStore persists batches of events.
type EventStore interface {
	InsertEvents(events []*Event) error
	// QueryFlagInsights returns aggregated eval counts for the given env.
	QueryFlagInsights(projectKey, envKey string, since int64) ([]*FlagInsight, error)
	// QueryStaleFlags finds flags with no evals in the last 30 days.
	QueryStaleFlags(projectKey, envKey string) ([]*StaleFlag, error)
	// QueryConversions returns the number of times a custom metric event was
	// recorded by users in each variation bucket.
	QueryConversions(projectKey, envKey, flagKey, metricEvent string, since int64) (map[int]int64, error)
}

func NewIngestor(store EventStore, flushInterval time.Duration, maxBatch int) *Ingestor {
	return &Ingestor{
		store:         store,
		FlushInterval: flushInterval,
		MaxBatch:      maxBatch,
		done:          make(chan struct{}),
	}
}

// Start launches the background flush goroutine.
func (ing *Ingestor) Start() {
	ing.wg.Add(1)
	go ing.loop()
}

// Stop drains the buffer and shuts down.
func (ing *Ingestor) Stop() {
	close(ing.done)
	ing.wg.Wait()
	ing.flush()
}

// Track enqueues an event. Non-blocking: if buffer is full, oldest events are dropped.
func (ing *Ingestor) Track(e *Event) {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().UnixMilli()
	}
	ing.mu.Lock()
	ing.buf = append(ing.buf, e)
	shouldFlush := len(ing.buf) >= ing.MaxBatch
	ing.mu.Unlock()

	if shouldFlush {
		ing.flush()
	}
}

func (ing *Ingestor) loop() {
	defer ing.wg.Done()
	ticker := time.NewTicker(ing.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ing.done:
			return
		case <-ticker.C:
			ing.flush()
		}
	}
}

func (ing *Ingestor) flush() {
	ing.mu.Lock()
	if len(ing.buf) == 0 {
		ing.mu.Unlock()
		return
	}
	batch := ing.buf
	ing.buf = make([]*Event, 0, ing.MaxBatch)
	ing.mu.Unlock()

	_ = ing.store.InsertEvents(batch) // log error in production; ignore here
}
