package main

import (
	"sync"
)

// Broker fans a new message out to every connected SSE client.
// Each client gets a buffered channel so one slow phone cannot block the room.
type Broker struct {
	mu      sync.Mutex
	clients map[chan Message]struct{}
}

func NewBroker() *Broker {
	return &Broker{clients: map[chan Message]struct{}{}}
}

func (b *Broker) Subscribe() chan Message {
	ch := make(chan Message, 32)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(ch chan Message) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

func (b *Broker) Publish(m Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- m:
		default:
			// Client is too slow / disconnected; drop this one message
			// for them rather than blocking everyone else.
		}
	}
}

func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}
