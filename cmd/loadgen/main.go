package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// LoadGen simulates N concurrent SDK clients, each evaluating a set of flags
// repeatedly. Outputs throughput metrics to stdout every 5 seconds.

var (
	baseURL   = flag.String("url", "http://localhost:8080", "server base URL")
	clients   = flag.Int("clients", 10, "number of concurrent SDK client goroutines")
	sdkKey    = flag.String("sdk-key", "sdk-server-default-prod", "SDK key to use")
	duration  = flag.Duration("duration", 60*time.Second, "how long to run")
	flagKeys  = []string{"my-flag", "dark-mode", "new-checkout", "feature-v2", "experiment-cta"}
	userCount = flag.Int("users", 1000, "number of simulated user keys to rotate through")
)

type EvalResponse struct {
	VariationIndex *int `json:"variationIndex"`
	Value          any  `json:"value"`
	Reason         struct {
		Kind string `json:"kind"`
	} `json:"reason"`
}

func main() {
	flag.Parse()

	var (
		totalEvals  atomic.Int64
		totalErrors atomic.Int64
	)

	stop := make(chan struct{})
	time.AfterFunc(*duration, func() { close(stop) })

	var wg sync.WaitGroup
	for i := 0; i < *clients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			rng := rand.New(rand.NewSource(int64(clientID)))
			for {
				select {
				case <-stop:
					return
				default:
				}
				userKey := fmt.Sprintf("user-%d", rng.Intn(*userCount))
				flagKey := flagKeys[rng.Intn(len(flagKeys))]

				if err := evalFlag(client, userKey, flagKey); err != nil {
					totalErrors.Add(1)
				} else {
					totalEvals.Add(1)
				}
				// Simulate realistic think time: 10-50ms between evaluations
				time.Sleep(time.Duration(10+rng.Intn(40)) * time.Millisecond)
			}
		}(i)
	}

	// Reporter
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	start := time.Now()
	lastCount := int64(0)

	for {
		select {
		case <-stop:
			wg.Wait()
			elapsed := time.Since(start)
			total := totalEvals.Load()
			errs := totalErrors.Load()
			fmt.Printf("\nFinal: %d evals in %s (%.0f/s), %d errors\n",
				total, elapsed.Round(time.Second), float64(total)/elapsed.Seconds(), errs)
			return

		case <-ticker.C:
			current := totalEvals.Load()
			errs := totalErrors.Load()
			delta := current - lastCount
			lastCount = current
			elapsed := time.Since(start)
			fmt.Printf("[%5.0fs] total=%7d (+%5d/5s = %5.0f/s) errors=%d clients=%d\n",
				elapsed.Seconds(), current, delta, float64(delta)/5.0, errs, *clients)
		}
	}
}

func evalFlag(client *http.Client, userKey, flagKey string) error {
	// Use the REST evaluation endpoint
	url := fmt.Sprintf("%s/sdk/v1/evaluate", *baseURL)
	body := map[string]any{
		"flagKey": flagKey,
		"context": map[string]any{"key": userKey, "kind": "user"},
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+*sdkKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}
