package pennant

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pennant/internal/eval"
	"pennant/internal/model"
	"pennant/internal/snapshot"
)

// Client is a feature flag SDK client. It maintains an in-memory snapshot
// updated via SSE streaming, and evaluates flags locally without network I/O.
// The snapshot pointer is updated atomically — reads never block writes.
type Client struct {
	opts       Options
	snap       atomic.Pointer[snapshot.Snapshot]
	httpClient *http.Client
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	ready      chan struct{}
	readyOnce  sync.Once
}

// Options configures a Client.
type Options struct {
	SDKKey      string
	BaseURL     string        // e.g. "http://localhost:8080"
	InitTimeout time.Duration // max wait for first snapshot; default 5s
}

// snapshotStore wraps a *snapshot.Snapshot as eval.Store.
type snapshotStore struct {
	snap *snapshot.Snapshot
}

func (s *snapshotStore) GetFlag(key string) (*model.Flag, *model.FlagConfig, bool) {
	if s.snap == nil {
		return nil, nil, false
	}
	rf, ok := s.snap.Flags[key]
	if !ok {
		return nil, nil, false
	}
	flag := &model.Flag{
		Key:        rf.Key,
		Name:       rf.Name,
		Description: rf.Description,
		Type:       rf.Type,
		Variations: rf.Variations,
		Tags:       rf.Tags,
		Temporary:  rf.Temporary,
		Archived:   rf.Archived,
	}
	return flag, rf.Config, true
}

func (s *snapshotStore) GetSegment(key string) (*model.Segment, bool) {
	if s.snap == nil {
		return nil, false
	}
	seg, ok := s.snap.Segments[key]
	return seg, ok
}

// New creates and initializes a Client. It starts a background SSE streaming
// goroutine and blocks until the first snapshot is received or InitTimeout
// elapses. On timeout it returns a non-nil client that will self-heal.
func New(opts Options) (*Client, error) {
	if opts.InitTimeout == 0 {
		opts.InitTimeout = 5 * time.Second
	}
	c := &Client{
		opts:       opts,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ready:      make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	c.wg.Add(1)
	go c.streamLoop(ctx)

	select {
	case <-c.ready:
		return c, nil
	case <-time.After(opts.InitTimeout):
		// Return the client anyway — it will self-heal in the background.
		return c, fmt.Errorf("pennant: init timeout after %s (will self-heal)", opts.InitTimeout)
	}
}

// Close shuts down the background streaming goroutine and waits for it to exit.
func (c *Client) Close() {
	c.cancel()
	c.wg.Wait()
}

// streamLoop connects to the SSE endpoint and processes events.
// On disconnect it retries with exponential backoff, capped at 30 s.
func (c *Client) streamLoop(ctx context.Context) {
	defer c.wg.Done()
	backoff := time.Second
	for {
		if err := c.connect(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			backoff = time.Duration(math.Min(float64(backoff*2), float64(30*time.Second)))
		}
	}
}

// connect opens one SSE connection and processes all events until the
// connection closes or ctx is cancelled.
func (c *Client) connect(ctx context.Context) error {
	url := c.opts.BaseURL + "/sdk/v1/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.SDKKey)
	req.Header.Set("Accept", "text/event-stream")

	// Send Last-Event-ID so the server can replay deltas rather than full snapshots.
	if snap := c.snap.Load(); snap != nil {
		req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", snap.Version))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pennant: stream returned %d", resp.StatusCode)
	}

	return c.parseSSE(ctx, resp.Body)
}

// parseSSE reads lines from an SSE stream and dispatches complete events.
func (c *Client) parseSSE(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	var event, data string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if line == "" {
			// Blank line signals end of one event block — dispatch it.
			if event != "" && data != "" {
				c.handleEvent(event, data)
			}
			event, data = "", ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return scanner.Err()
}

func (c *Client) handleEvent(event, data string) {
	switch event {
	case "put":
		var snap snapshot.Snapshot
		if err := json.Unmarshal([]byte(data), &snap); err != nil {
			return
		}
		c.snap.Store(&snap)
		c.readyOnce.Do(func() { close(c.ready) })

	case "patch":
		var delta snapshot.Delta
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			return
		}
		current := c.snap.Load()
		if current == nil {
			return
		}
		next := snapshot.ApplyDelta(current, &delta)
		c.snap.Store(next)
		c.readyOnce.Do(func() { close(c.ready) })
	}
}

// BoolVariation evaluates a boolean flag. Returns defaultValue if the flag is
// not found, the client is not ready, or the value is not a boolean.
func (c *Client) BoolVariation(flagKey string, ctx model.Context, defaultValue bool) bool {
	r := c.evalFlag(flagKey, ctx)
	if !r.ready || r.idx == nil {
		return defaultValue
	}
	b, ok := r.value.(bool)
	if !ok {
		return defaultValue
	}
	return b
}

// StringVariation evaluates a string flag. Returns defaultValue on any error.
func (c *Client) StringVariation(flagKey string, ctx model.Context, defaultValue string) string {
	r := c.evalFlag(flagKey, ctx)
	if !r.ready || r.idx == nil {
		return defaultValue
	}
	s, ok := r.value.(string)
	if !ok {
		return defaultValue
	}
	return s
}

// Float64Variation evaluates a number flag. Returns defaultValue on any error.
func (c *Client) Float64Variation(flagKey string, ctx model.Context, defaultValue float64) float64 {
	r := c.evalFlag(flagKey, ctx)
	if !r.ready || r.idx == nil {
		return defaultValue
	}
	switch n := r.value.(type) {
	case float64:
		return n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return defaultValue
		}
		return f
	}
	return defaultValue
}

// JSONVariation evaluates a JSON flag and unmarshals the result into target.
// Returns an error if the flag is not found, the client is not ready, or
// unmarshaling fails.
func (c *Client) JSONVariation(flagKey string, ctx model.Context, target any) error {
	r := c.evalFlag(flagKey, ctx)
	if !r.ready || r.idx == nil {
		return fmt.Errorf("pennant: flag %q not found or client not ready", flagKey)
	}
	b, err := json.Marshal(r.value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

// EvaluateDetail returns the full evaluation result including the variation
// index, the resolved value, and the reason for the result.
func (c *Client) EvaluateDetail(flagKey string, ctx model.Context) (*int, any, model.Reason) {
	return c.evaluate(flagKey, ctx)
}

// evalResult bundles a variation index + value so variation helpers can
// use a single call that returns two usable typed results.
type evalResult struct {
	idx   *int
	value any
	ready bool // false means "not found / not ready"
}

func (c *Client) evalFlag(flagKey string, ctx model.Context) evalResult {
	snap := c.snap.Load()
	if snap == nil {
		return evalResult{}
	}
	store := &snapshotStore{snap: snap}
	flag, cfg, ok := store.GetFlag(flagKey)
	if !ok {
		return evalResult{}
	}
	idx, val, _ := eval.Evaluate(flag, cfg, &ctx, store)
	return evalResult{idx: idx, value: val, ready: true}
}

func (c *Client) evaluate(flagKey string, ctx model.Context) (*int, any, model.Reason) {
	snap := c.snap.Load()
	if snap == nil {
		return nil, nil, model.Reason{Kind: model.ReasonError, ErrorKind: model.ErrClientNotReady}
	}
	store := &snapshotStore{snap: snap}
	flag, cfg, ok := store.GetFlag(flagKey)
	if !ok {
		return nil, nil, model.Reason{Kind: model.ReasonError, ErrorKind: model.ErrFlagNotFound}
	}
	return eval.Evaluate(flag, cfg, &ctx, store)
}

// IsReady returns true if the client has received at least one snapshot.
func (c *Client) IsReady() bool {
	select {
	case <-c.ready:
		return true
	default:
		return false
	}
}

// Snapshot returns the current in-memory snapshot. May be nil before the
// first SSE event is received.
func (c *Client) Snapshot() *snapshot.Snapshot {
	return c.snap.Load()
}
