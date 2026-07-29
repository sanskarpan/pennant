package stream

import "time"

// Message is an SSE event published through the hub.
type Message struct {
	Event   string // "put", "patch", "delete"
	Version int64
	Data    any
}

// Subscriber represents a connected SSE client.
type Subscriber struct {
	ID          string
	EnvKey      string
	ProjectKey  string
	SDKKey      string
	Ch          chan Message // buffered, capacity 32
	LastSent    int64
	ConnectedAt time.Time
	UserAgent   string
	SDKVersion  string
}
