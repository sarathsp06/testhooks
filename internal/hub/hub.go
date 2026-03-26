package hub

import (
	"encoding/json"
	"sync"
	"time"
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

// pendingEntry tracks a pending response with its creation time for TTL sweeping.
type pendingEntry struct {
	ch        chan ResponseResult
	slug      string // M-04: scoped to slug for validation
	createdAt time.Time
}

// timestampedMessage wraps a Message with a timestamp for TTL sweeping.
type timestampedMessage struct {
	msg       Message
	createdAt time.Time
}

// Hub manages per-slug pub/sub and ring buffers for browser-mode endpoints.
type Hub struct {
	mu                 sync.RWMutex
	clients            map[string]map[*client]struct{} // slug -> set of clients
	buffers            map[string]*ringBuffer          // slug -> ring buffer (browser-mode)
	pending            map[string]*pendingEntry        // requestID -> pending response entry
	pendingMu          sync.Mutex
	ringBufferSize     int
	ringBufferTTL      time.Duration // H-04: TTL for ring buffer entries
	maxSubscribersSlug int           // M-09: max subscribers per slug
	stopSweep          chan struct{}
}

// ringBuffer is a fixed-size circular buffer of timestamped Messages.
type ringBuffer struct {
	mu   sync.Mutex
	msgs []timestampedMessage
	size int
	pos  int
	full bool
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		msgs: make([]timestampedMessage, size),
		size: size,
	}
}

func (rb *ringBuffer) push(m Message) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.msgs[rb.pos] = timestampedMessage{msg: m, createdAt: time.Now()}
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
			out = append(out, rb.msgs[i].msg)
		}
		for i := 0; i < rb.pos; i++ {
			out = append(out, rb.msgs[i].msg)
		}
	} else {
		out = make([]Message, 0, rb.pos)
		for i := 0; i < rb.pos; i++ {
			out = append(out, rb.msgs[i].msg)
		}
	}
	return out
}

// sweepExpired removes entries older than maxAge. H-04: TTL sweep for ring buffers.
func (rb *ringBuffer) sweepExpired(maxAge time.Duration) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)

	// Zero out expired entries. Since the ring buffer is circular,
	// we mark expired slots as zero-value. This is best-effort cleanup.
	for i := range rb.msgs {
		if !rb.msgs[i].createdAt.IsZero() && rb.msgs[i].createdAt.Before(cutoff) {
			rb.msgs[i] = timestampedMessage{}
		}
	}
}

// New creates a new Hub with the given ring buffer size for browser-mode endpoints.
// H-04: ringBufferTTLSeconds controls how long entries live before being swept.
// M-09: maxSubscribersPerSlug limits concurrent subscribers per slug.
func New(ringBufferSize int, ringBufferTTLSeconds int, maxSubscribersPerSlug int) *Hub {
	if ringBufferSize <= 0 {
		ringBufferSize = 100
	}
	if ringBufferTTLSeconds <= 0 {
		ringBufferTTLSeconds = 300 // 5 minutes
	}
	if maxSubscribersPerSlug <= 0 {
		maxSubscribersPerSlug = 50
	}
	h := &Hub{
		clients:            make(map[string]map[*client]struct{}),
		buffers:            make(map[string]*ringBuffer),
		pending:            make(map[string]*pendingEntry),
		ringBufferSize:     ringBufferSize,
		ringBufferTTL:      time.Duration(ringBufferTTLSeconds) * time.Second,
		maxSubscribersSlug: maxSubscribersPerSlug,
		stopSweep:          make(chan struct{}),
	}

	// H-04: Start background goroutine to sweep expired ring buffer entries.
	// M-10: Also sweeps stale pending response entries.
	go h.sweepLoop()

	return h
}

// sweepLoop periodically cleans up expired ring buffer entries and stale pending responses.
func (h *Hub) sweepLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopSweep:
			return
		case <-ticker.C:
			h.sweepRingBuffers()
			h.sweepPending()
		}
	}
}

// sweepRingBuffers removes expired entries from all ring buffers. H-04.
func (h *Hub) sweepRingBuffers() {
	h.mu.RLock()
	buffers := make(map[string]*ringBuffer, len(h.buffers))
	for k, v := range h.buffers {
		buffers[k] = v
	}
	h.mu.RUnlock()

	for _, rb := range buffers {
		rb.sweepExpired(h.ringBufferTTL)
	}
}

// sweepPending removes stale pending response entries (older than 60s). M-10.
func (h *Hub) sweepPending() {
	cutoff := time.Now().Add(-60 * time.Second)
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	for id, entry := range h.pending {
		if entry.createdAt.Before(cutoff) {
			delete(h.pending, id)
		}
	}
}

// Stop terminates the background sweep goroutine.
func (h *Hub) Stop() {
	close(h.stopSweep)
}

// Subscribe registers a client for the given slug. Returns a channel
// for receiving messages and a cleanup function.
// M-09: Returns nil, nil if the subscriber limit for this slug is reached.
func (h *Hub) Subscribe(slug string, bufferSize int) (<-chan Message, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// M-09: Enforce max subscribers per slug.
	if len(h.clients[slug]) >= h.maxSubscribersSlug {
		return nil, nil
	}

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
// M-04: slug is stored so DeliverResponse can validate scope.
// M-10: createdAt enables TTL sweeping of stale entries.
// Returns the channel and a cleanup function. The caller must always call cleanup.
func (h *Hub) WaitForResponse(slug string, requestID string) (<-chan ResponseResult, func()) {
	ch := make(chan ResponseResult, 1)
	h.pendingMu.Lock()
	h.pending[requestID] = &pendingEntry{
		ch:        ch,
		slug:      slug,
		createdAt: time.Now(),
	}
	h.pendingMu.Unlock()

	cleanup := func() {
		h.pendingMu.Lock()
		delete(h.pending, requestID)
		h.pendingMu.Unlock()
	}
	return ch, cleanup
}

// DeliverResponse sends a browser-computed response to the waiting capture handler.
// M-04: Validates that the response is scoped to the correct slug.
// Returns true if a handler was waiting, false if no one was listening (timed out).
func (h *Hub) DeliverResponse(slug string, requestID string, result ResponseResult) bool {
	h.pendingMu.Lock()
	entry, ok := h.pending[requestID]
	h.pendingMu.Unlock()
	if !ok {
		return false
	}
	// M-04: Validate slug scope — prevent cross-slug response injection.
	if entry.slug != slug {
		return false
	}
	select {
	case entry.ch <- result:
		return true
	default:
		return false
	}
}
