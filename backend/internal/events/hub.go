// Package events provides a simple fan-out event hub for broadcasting
// state changes (e.g. libvirt VM lifecycle events) to multiple subscribers
// (typically SSE HTTP handlers).
//
// Usage:
//
//	hub := events.NewHub()
//	go hub.Run(ctx)  // not strictly required, but useful for graceful shutdown
//
//	ch := hub.Subscribe()
//	for e := range ch {
//	    // handle event
//	}
//
//	hub.Broadcast(events.Event{Type: "vm.state", VmID: "...", State: "running"})
package events

import (
	"context"
	"sync"
	"sync/atomic"
)

// Event is the wire format for a single broadcast message.
// Fields are JSON-tagged so they serialize directly into SSE `data:` lines.
type Event struct {
	Type      string `json:"type"`        // e.g. "vm.state", "vm.removed", "vm.metrics"
	VmID      string `json:"vm_id"`       // libvirt UUID
	State     string `json:"state"`       // "running" | "shutoff" | "paused" | "crashed" | "unknown"
	Name      string `json:"name"`        // VM name (convenience for the UI)
	Timestamp int64  `json:"timestamp"`   // unix seconds
	Data      any    `json:"data,omitempty"` // optional payload (e.g. metrics series)
}

// Hub is a thread-safe broadcaster. Zero value is not usable; use NewHub.
type Hub struct {
	mu      sync.RWMutex
	clients map[uint64]chan Event
	nextID  uint64
	closed  atomic.Bool
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[uint64]chan Event),
	}
}

// Subscribe registers a new client. The returned channel is buffered; if
// the consumer falls behind, events are dropped (non-blocking send) to
// avoid stalling broadcasters.
//
// The returned cancel function removes the client and closes its channel.
func (h *Hub) Subscribe() (id uint64, ch chan Event, cancel func()) {
	id = atomic.AddUint64(&h.nextID, 1)
	ch = make(chan Event, 16)
	h.mu.Lock()
	h.clients[id] = ch
	h.mu.Unlock()

	cancel = func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c, ok := h.clients[id]; ok {
			delete(h.clients, id)
			close(c)
		}
	}
	return id, ch, cancel
}

// Broadcast sends an event to every subscribed client. Non-blocking:
// if a client's buffer is full the event is dropped for that client.
func (h *Hub) Broadcast(e Event) {
	if h.closed.Load() {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.clients {
		select {
		case ch <- e:
		default:
			// Drop event for slow consumer
		}
	}
}

// ClientCount returns the number of active subscribers.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Run blocks until ctx is cancelled. It exists so callers can hold a single
// reference to the hub and let it clean up on shutdown. Calling Run is
// optional — Broadcast and Subscribe are safe to call from any goroutine
// at any time.
func (h *Hub) Run(ctx context.Context) {
	<-ctx.Done()
	h.Close()
}

// Close disconnects all clients. After Close, Broadcast is a no-op.
func (h *Hub) Close() {
	if h.closed.Swap(true) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.clients {
		delete(h.clients, id)
		close(ch)
	}
}
