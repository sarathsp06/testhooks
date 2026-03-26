package hub

import (
	"encoding/json"
	"testing"
	"time"
)

func makeMsg(typ, data string) Message {
	return Message{Type: typ, Data: json.RawMessage(data)}
}

func TestSubscribeAndPublish(t *testing.T) {
	h := New(100, 300, 50)

	ch, cleanup := h.Subscribe("test-slug", 0)
	defer cleanup()

	msg := makeMsg("request", `{"id":"r1"}`)
	h.Publish("test-slug", msg, false)

	select {
	case received := <-ch:
		if received.Type != "request" {
			t.Errorf("Type = %q, want %q", received.Type, "request")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublishToMultipleSubscribers(t *testing.T) {
	h := New(100, 300, 50)

	ch1, cleanup1 := h.Subscribe("slug", 0)
	defer cleanup1()
	ch2, cleanup2 := h.Subscribe("slug", 0)
	defer cleanup2()

	msg := makeMsg("request", `{"id":"r1"}`)
	h.Publish("slug", msg, false)

	for i, ch := range []<-chan Message{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Type != "request" {
				t.Errorf("subscriber %d: Type = %q, want %q", i, received.Type, "request")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestPublishToWrongSlug(t *testing.T) {
	h := New(100, 300, 50)

	ch, cleanup := h.Subscribe("slug-a", 0)
	defer cleanup()

	msg := makeMsg("request", `{"id":"r1"}`)
	h.Publish("slug-b", msg, false)

	select {
	case <-ch:
		t.Fatal("should not receive message for different slug")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestCleanupUnsubscribes(t *testing.T) {
	h := New(100, 300, 50)

	ch, cleanup := h.Subscribe("slug", 0)
	cleanup()

	// After cleanup, channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after cleanup")
	}

	if h.HasSubscribers("slug") {
		t.Error("HasSubscribers = true after cleanup, want false")
	}
}

func TestHasSubscribers(t *testing.T) {
	h := New(100, 300, 50)

	if h.HasSubscribers("slug") {
		t.Error("HasSubscribers = true with no subscribers")
	}

	_, cleanup := h.Subscribe("slug", 0)
	if !h.HasSubscribers("slug") {
		t.Error("HasSubscribers = false after subscribe")
	}

	cleanup()
	if h.HasSubscribers("slug") {
		t.Error("HasSubscribers = true after unsubscribe")
	}
}

func TestRingBuffer_PushAndDrain(t *testing.T) {
	rb := newRingBuffer(3)

	// Empty buffer.
	if msgs := rb.drain(); msgs != nil {
		t.Errorf("drain empty buffer = %v, want nil", msgs)
	}

	rb.push(makeMsg("request", `{"id":"1"}`))
	rb.push(makeMsg("request", `{"id":"2"}`))

	msgs := rb.drain()
	if len(msgs) != 2 {
		t.Fatalf("drain = %d messages, want 2", len(msgs))
	}
}

func TestRingBuffer_Wrap(t *testing.T) {
	rb := newRingBuffer(3)

	// Push 5 messages into a buffer of size 3 — should keep last 3.
	for i := 0; i < 5; i++ {
		rb.push(makeMsg("request", `{"id":"`+string(rune('0'+i))+`"}`))
	}

	msgs := rb.drain()
	if len(msgs) != 3 {
		t.Fatalf("drain = %d messages, want 3", len(msgs))
	}
}

func TestPublishWithBuffer(t *testing.T) {
	h := New(100, 300, 50)

	// Publish with buffer before any subscriber.
	msg1 := makeMsg("request", `{"id":"r1"}`)
	msg2 := makeMsg("request", `{"id":"r2"}`)
	h.Publish("slug", msg1, true)
	h.Publish("slug", msg2, true)

	// Now subscribe — should receive buffered messages.
	ch, cleanup := h.Subscribe("slug", 0)
	defer cleanup()

	received := 0
	timeout := time.After(time.Second)
	for {
		select {
		case <-ch:
			received++
			if received == 2 {
				return // success
			}
		case <-timeout:
			t.Fatalf("received %d buffered messages, want 2", received)
		}
	}
}

func TestRemoveBuffer(t *testing.T) {
	h := New(100, 300, 50)

	// Publish with buffer.
	h.Publish("slug", makeMsg("request", `{"id":"r1"}`), true)
	h.RemoveBuffer("slug")

	// Subscribe after buffer removed — should get nothing from buffer.
	ch, cleanup := h.Subscribe("slug", 0)
	defer cleanup()

	select {
	case <-ch:
		t.Fatal("should not receive message after buffer removed")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestWaitForResponse_DeliverSuccess(t *testing.T) {
	h := New(100, 300, 50)

	ch, cleanup := h.WaitForResponse("test-slug", "req-123")
	defer cleanup()

	result := ResponseResult{
		RequestID: "req-123",
		Status:    201,
		Body:      "created",
	}

	delivered := h.DeliverResponse("test-slug", "req-123", result)
	if !delivered {
		t.Fatal("DeliverResponse returned false")
	}

	select {
	case r := <-ch:
		if r.Status != 201 {
			t.Errorf("Status = %d, want 201", r.Status)
		}
		if r.Body != "created" {
			t.Errorf("Body = %q, want %q", r.Body, "created")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestWaitForResponse_DeliverToUnknownRequest(t *testing.T) {
	h := New(100, 300, 50)

	result := ResponseResult{RequestID: "unknown"}
	delivered := h.DeliverResponse("test-slug", "unknown", result)
	if delivered {
		t.Error("DeliverResponse returned true for unknown request")
	}
}

func TestWaitForResponse_Cleanup(t *testing.T) {
	h := New(100, 300, 50)

	_, cleanup := h.WaitForResponse("test-slug", "req-456")
	cleanup()

	// After cleanup, delivering should fail.
	result := ResponseResult{RequestID: "req-456"}
	delivered := h.DeliverResponse("test-slug", "req-456", result)
	if delivered {
		t.Error("DeliverResponse returned true after cleanup")
	}
}
