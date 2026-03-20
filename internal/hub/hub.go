package hub

import (
	"encoding/json"
	"sync"
)

// Message is a JSON-serializable event pushed to WebSocket clients.
type Message struct {
	Type string          `json:"type"` // "request", "response_needed", "endpoint_updated", "endpoint_deleted"
	Data json.RawMessage `json:"data"`
}

// ResponseResult is sent back from the browser with the computed response.
type ResponseResult struct {
	RequestID   string            `json:"request_id"`
	Status      int               `json:"status,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
}

// client represents a single WebSocket subscriber.
type client struct {
	ch chan Message
}

// Hub manages per-slug pub/sub and ring buffers for browser-mode endpoints.
type Hub struct {
	mu             sync.RWMutex
	clients        map[string]map[*client]struct{} // slug -> set of clients
	buffers        map[string]*ringBuffer          // slug -> ring buffer (browser-mode)
	pending        map[string]chan ResponseResult  // requestID -> response channel
	pendingMu      sync.Mutex
	ringBufferSize int
}

// ringBuffer is a fixed-size circular buffer of Messages.
type ringBuffer struct {
	mu   sync.Mutex
	msgs []Message
	size int
	pos  int
	full bool
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		msgs: make([]Message, size),
		size: size,
	}
}

func (rb *ringBuffer) push(m Message) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.msgs[rb.pos] = m
	rb.pos = (rb.pos + 1) % rb.size
	if rb.pos == 0 {
		rb.full = true
	}
}

func (rb *ringBuffer) drain() []Message {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if !rb.full && rb.pos == 0 {
		return nil
	}
	var out []Message
	if rb.full {
		// Read from pos (oldest) to end, then from 0 to pos-1
		out = make([]Message, 0, rb.size)
		for i := rb.pos; i < rb.size; i++ {
			out = append(out, rb.msgs[i])
		}
		for i := 0; i < rb.pos; i++ {
			out = append(out, rb.msgs[i])
		}
	} else {
		out = make([]Message, rb.pos)
		copy(out, rb.msgs[:rb.pos])
	}
	return out
}

// New creates a new Hub with the given ring buffer size for browser-mode endpoints.
// If ringBufferSize is <= 0, it defaults to 100.
func New(ringBufferSize int) *Hub {
	if ringBufferSize <= 0 {
		ringBufferSize = 100
	}
	return &Hub{
		clients:        make(map[string]map[*client]struct{}),
		buffers:        make(map[string]*ringBuffer),
		pending:        make(map[string]chan ResponseResult),
		ringBufferSize: ringBufferSize,
	}
}

// Subscribe registers a client for the given slug. Returns a channel
// for receiving messages and a cleanup function.
func (h *Hub) Subscribe(slug string, bufferSize int) (<-chan Message, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c := &client{ch: make(chan Message, 64)}
	if h.clients[slug] == nil {
		h.clients[slug] = make(map[*client]struct{})
	}
	h.clients[slug][c] = struct{}{}

	// Flush ring buffer to new subscriber (browser-mode catch-up).
	if rb, ok := h.buffers[slug]; ok {
		msgs := rb.drain()
		for _, m := range msgs {
			select {
			case c.ch <- m:
			default:
			}
		}
	}

	cleanup := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.clients[slug], c)
		if len(h.clients[slug]) == 0 {
			delete(h.clients, slug)
		}
		close(c.ch)
	}

	return c.ch, cleanup
}

// Publish sends a message to all subscribers of the given slug.
// If useBuffer is true, the message is also stored in the ring buffer
// (used for browser-mode endpoints so reconnecting clients get catch-up).
func (h *Hub) Publish(slug string, msg Message, useBuffer bool) {
	// Use a write lock for the entire operation to avoid RLock→Unlock→Lock
	// upgrade races that can occur when creating a ring buffer on first use.
	h.mu.Lock()
	defer h.mu.Unlock()

	if useBuffer {
		rb, ok := h.buffers[slug]
		if !ok {
			rb = newRingBuffer(h.ringBufferSize)
			h.buffers[slug] = rb
		}
		rb.push(msg)
	}

	for c := range h.clients[slug] {
		select {
		case c.ch <- msg:
		default:
			// Slow consumer — drop message.
		}
	}
}

// HasSubscribers returns true if any clients are listening on the slug.
func (h *Hub) HasSubscribers(slug string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[slug]) > 0
}

// RemoveBuffer removes the ring buffer for a slug (called when endpoint is deleted).
func (h *Hub) RemoveBuffer(slug string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.buffers, slug)
}

// WaitForResponse creates a channel that the capture handler can block on
// while waiting for the browser to send back a computed response.
// Returns the channel and a cleanup function. The caller must always call cleanup.
func (h *Hub) WaitForResponse(requestID string) (<-chan ResponseResult, func()) {
	ch := make(chan ResponseResult, 1)
	h.pendingMu.Lock()
	h.pending[requestID] = ch
	h.pendingMu.Unlock()

	cleanup := func() {
		h.pendingMu.Lock()
		delete(h.pending, requestID)
		h.pendingMu.Unlock()
	}
	return ch, cleanup
}

// DeliverResponse sends a browser-computed response to the waiting capture handler.
// Returns true if a handler was waiting, false if no one was listening (timed out).
func (h *Hub) DeliverResponse(requestID string, result ResponseResult) bool {
	h.pendingMu.Lock()
	ch, ok := h.pending[requestID]
	h.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- result:
		return true
	default:
		return false
	}
}
