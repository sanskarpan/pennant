package events

import "sync"

type Bus struct {
	mu   sync.RWMutex
	subs map[string][]chan any
}

func NewBus() *Bus {
	return &Bus{subs: make(map[string][]chan any)}
}

func (b *Bus) Subscribe(topic string, bufSize int) <-chan any {
	ch := make(chan any, bufSize)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()
	return ch
}

func (b *Bus) Unsubscribe(topic string, ch <-chan any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[topic]
	for i, s := range subs {
		if s == ch {
			b.subs[topic] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (b *Bus) Publish(topic string, payload any) {
	b.mu.RLock()
	subs := b.subs[topic]
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- payload:
		default:
		}
	}
}
