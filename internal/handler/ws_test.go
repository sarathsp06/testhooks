package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/hub"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// setupWSTest creates a mock store, hub, WS handler, and httptest.Server.
// The server is set up with the route pattern GET /ws/{slug} matching the WS handler.
// Returns the server URL (ws://...) and a cleanup function.
func setupWSTest(t *testing.T, store *mockStore) (serverURL string, h *hub.Hub, cleanup func()) {
	t.Helper()
	testHub := hub.New(100, 300, 50)
	logger := zerolog.Nop()
	wsHandler := NewWS(store, testHub, logger, []string{"*"})

	mux := http.NewServeMux()
	mux.Handle("GET /ws/{slug}", wsHandler)

	server := httptest.NewServer(mux)

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	return wsURL, testHub, func() {
		server.Close()
	}
}

func TestWS_MissingSlug(t *testing.T) {
	store := newMockStore()
	logger := zerolog.Nop()
	testHub := hub.New(100, 300, 50)
	wsHandler := NewWS(store, testHub, logger, []string{"*"})

	// Request without slug — the handler checks PathValue("slug") == ""
	req := httptest.NewRequest("GET", "/ws/", nil)
	w := httptest.NewRecorder()
	wsHandler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "missing slug") {
		t.Errorf("expected 'missing slug' in body, got %q", w.Body.String())
	}
}

func TestWS_EndpointNotFound(t *testing.T) {
	store := newMockStore()
	logger := zerolog.Nop()
	testHub := hub.New(100, 300, 50)
	wsHandler := NewWS(store, testHub, logger, []string{"*"})

	// Use a mux so PathValue works.
	mux := http.NewServeMux()
	mux.Handle("GET /ws/{slug}", wsHandler)

	req := httptest.NewRequest("GET", "/ws/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "endpoint not found") {
		t.Errorf("expected 'endpoint not found' in body, got %q", w.Body.String())
	}
}

func TestWS_SuccessfulConnection_ReceivesMessages(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "test-slug", "Test", "server", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	// Connect via WebSocket.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/test-slug", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	// Give the subscription a moment to register.
	time.Sleep(50 * time.Millisecond)

	// Publish a message to the hub.
	data, _ := json.Marshal(map[string]string{"method": "POST", "body": "hello"})
	testHub.Publish("test-slug", hub.Message{
		Type: "request",
		Data: json.RawMessage(data),
	}, false)

	// Read the message from the WebSocket.
	var received hub.Message
	err = wsjson.Read(ctx, conn, &received)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if received.Type != "request" {
		t.Errorf("expected type 'request', got %q", received.Type)
	}

	// Verify the data matches.
	var body map[string]string
	if err := json.Unmarshal(received.Data, &body); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if body["method"] != "POST" {
		t.Errorf("expected method POST, got %q", body["method"])
	}
	if body["body"] != "hello" {
		t.Errorf("expected body 'hello', got %q", body["body"])
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_MultipleMessages(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "multi-slug", "Multi", "server", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/multi-slug", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Publish 3 messages.
	for i := 0; i < 3; i++ {
		data, _ := json.Marshal(map[string]int{"seq": i})
		testHub.Publish("multi-slug", hub.Message{
			Type: "request",
			Data: json.RawMessage(data),
		}, false)
	}

	// Read all 3.
	for i := 0; i < 3; i++ {
		var msg hub.Message
		err := wsjson.Read(ctx, conn, &msg)
		if err != nil {
			t.Fatalf("read %d failed: %v", i, err)
		}
		var body map[string]int
		json.Unmarshal(msg.Data, &body)
		if body["seq"] != i {
			t.Errorf("expected seq %d, got %d", i, body["seq"])
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_ResponseResult_DeliveredToHub(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "resp-slug", "Resp", "browser", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/resp-slug", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Set up a pending response in the hub (simulating a capture handler waiting).
	respCh, respCleanup := testHub.WaitForResponse("resp-slug", "req-123")
	defer respCleanup()

	// Send a response_result message from the "browser" (our WS client).
	resultData, _ := json.Marshal(hub.ResponseResult{
		RequestID:   "req-123",
		Status:      201,
		Headers:     map[string]string{"X-Custom": "yes"},
		Body:        `{"created":true}`,
		ContentType: "application/json",
	})

	msg := map[string]interface{}{
		"type": "response_result",
		"data": json.RawMessage(resultData),
	}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("write response_result failed: %v", err)
	}

	// The hub should deliver the response to the waiting channel.
	select {
	case result := <-respCh:
		if result.RequestID != "req-123" {
			t.Errorf("expected request_id 'req-123', got %q", result.RequestID)
		}
		if result.Status != 201 {
			t.Errorf("expected status 201, got %d", result.Status)
		}
		if result.Headers["X-Custom"] != "yes" {
			t.Errorf("expected header X-Custom=yes, got %q", result.Headers["X-Custom"])
		}
		if result.Body != `{"created":true}` {
			t.Errorf("unexpected body: %q", result.Body)
		}
		if result.ContentType != "application/json" {
			t.Errorf("expected content_type application/json, got %q", result.ContentType)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for response_result delivery")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_ResponseResult_NoWaitingHandler(t *testing.T) {
	// When no capture handler is waiting, response_result should be silently dropped.
	store := newMockStore()
	store.seedEndpoint("ep-1", "no-wait", "NoWait", "browser", nil)

	wsURL, _, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/no-wait", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Send a response_result with no matching pending request — should not panic.
	resultData, _ := json.Marshal(hub.ResponseResult{
		RequestID: "orphan-req",
		Status:    200,
		Body:      "ignored",
	})
	msg := map[string]interface{}{
		"type": "response_result",
		"data": json.RawMessage(resultData),
	}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Give it a moment to process. No panic = success.
	time.Sleep(100 * time.Millisecond)

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_ResponseResult_MissingRequestID(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "bad-resp", "BadResp", "browser", nil)

	wsURL, _, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/bad-resp", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Send a response_result without request_id.
	resultData, _ := json.Marshal(hub.ResponseResult{
		Status: 200,
		Body:   "no id",
	})
	msg := map[string]interface{}{
		"type": "response_result",
		"data": json.RawMessage(resultData),
	}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Should be warned and ignored — no panic.
	time.Sleep(100 * time.Millisecond)

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_UnknownMessageType_Ignored(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "unknown-type", "Unknown", "server", nil)

	wsURL, _, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/unknown-type", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Send an unknown message type — should be silently ignored.
	msg := map[string]interface{}{
		"type": "some_unknown_type",
		"data": json.RawMessage(`{"foo":"bar"}`),
	}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// No panic, no error = success.
	time.Sleep(100 * time.Millisecond)

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_ClientDisconnect_CleansUpSubscription(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "dc-slug", "DC", "server", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/dc-slug", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Verify there's a subscriber.
	if !testHub.HasSubscribers("dc-slug") {
		t.Fatal("expected hub to have subscriber after connect")
	}

	// Disconnect.
	conn.Close(websocket.StatusNormalClosure, "bye")

	// Wait for the server to process the disconnect.
	time.Sleep(200 * time.Millisecond)

	// Subscriber should be cleaned up.
	if testHub.HasSubscribers("dc-slug") {
		t.Error("expected hub to have no subscribers after disconnect")
	}
}

func TestWS_MultipleClients_SameSlug(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "shared", "Shared", "server", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect two clients to the same slug.
	conn1, _, err := websocket.Dial(ctx, wsURL+"/ws/shared", nil)
	if err != nil {
		t.Fatalf("dial conn1 failed: %v", err)
	}
	defer conn1.CloseNow()

	conn2, _, err := websocket.Dial(ctx, wsURL+"/ws/shared", nil)
	if err != nil {
		t.Fatalf("dial conn2 failed: %v", err)
	}
	defer conn2.CloseNow()

	time.Sleep(50 * time.Millisecond)

	// Publish one message.
	data, _ := json.Marshal(map[string]string{"msg": "broadcast"})
	testHub.Publish("shared", hub.Message{
		Type: "request",
		Data: json.RawMessage(data),
	}, false)

	// Both clients should receive it.
	for i, conn := range []*websocket.Conn{conn1, conn2} {
		var msg hub.Message
		err := wsjson.Read(ctx, conn, &msg)
		if err != nil {
			t.Fatalf("client %d read failed: %v", i+1, err)
		}
		if msg.Type != "request" {
			t.Errorf("client %d: expected type 'request', got %q", i+1, msg.Type)
		}
	}

	conn1.Close(websocket.StatusNormalClosure, "done")
	conn2.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_BrowserMode_ReceivesBufferedMessages(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "buffered", "Buffered", "browser", nil)

	wsURL, testHub, cleanup := setupWSTest(t, store)
	defer cleanup()

	// Publish messages BEFORE any client connects (with useBuffer=true for browser mode).
	for i := 0; i < 3; i++ {
		data, _ := json.Marshal(map[string]int{"seq": i})
		testHub.Publish("buffered", hub.Message{
			Type: "request",
			Data: json.RawMessage(data),
		}, true) // useBuffer=true
	}

	// Now connect — should receive the buffered messages.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL+"/ws/buffered", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.CloseNow()

	// Read all 3 buffered messages.
	for i := 0; i < 3; i++ {
		var msg hub.Message
		err := wsjson.Read(ctx, conn, &msg)
		if err != nil {
			t.Fatalf("read buffered %d failed: %v", i, err)
		}
		var body map[string]int
		json.Unmarshal(msg.Data, &body)
		if body["seq"] != i {
			t.Errorf("expected seq %d, got %d", i, body["seq"])
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWS_GetEndpointBySlug_DBError(t *testing.T) {
	store := newMockStore()
	store.seedEndpoint("ep-1", "err-slug", "Err", "server", nil)
	store.getEndpointBySlugErr = fmt.Errorf("db connection lost")

	logger := zerolog.Nop()
	testHub := hub.New(100, 300, 50)
	wsHandler := NewWS(store, testHub, logger, []string{"*"})

	mux := http.NewServeMux()
	mux.Handle("GET /ws/{slug}", wsHandler)

	req := httptest.NewRequest("GET", "/ws/err-slug", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// DB error should return 404 (endpoint not found).
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 on DB error, got %d", w.Code)
	}
}
