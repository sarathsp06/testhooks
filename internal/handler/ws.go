package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/sarathsp06/testhooks/internal/hub"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// WS handles WebSocket connections for live-streaming captured requests.
type WS struct {
	db             Store
	hub            *hub.Hub
	log            zerolog.Logger
	allowedOrigins []string
}

// NewWS creates a new WebSocket handler.
// allowedOrigins controls the Origin header check on WebSocket upgrade.
// Pass []string{"*"} to accept any origin (dev mode only).
func NewWS(store Store, h *hub.Hub, log zerolog.Logger, allowedOrigins []string) *WS {
	return &WS{
		db:             store,
		hub:            h,
		log:            log.With().Str("component", "ws").Logger(),
		allowedOrigins: allowedOrigins,
	}
}

// incomingMessage is the structure for messages sent from browser to server.
type incomingMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ServeHTTP upgrades the connection to WebSocket and streams events.
func (ws *WS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}

	// Verify endpoint exists.
	_, err := ws.db.GetEndpointBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: ws.allowedOrigins,
	})
	if err != nil {
		ws.log.Error().Err(err).Msg("websocket accept failed")
		return
	}
	defer conn.CloseNow()

	// MED-004: Set a read limit to prevent oversized messages from consuming memory.
	// 1MB is generous for control messages (response_result payloads).
	conn.SetReadLimit(1 << 20) // 1 MB

	ws.log.Info().Str("slug", slug).Msg("client connected")

	// Subscribe to hub for this slug.
	ch, cleanup := ws.hub.Subscribe(slug, 100)
	defer cleanup()

	// Use a dedicated context that we cancel when the connection is done.
	// This prevents goroutine leaks and races with the HTTP request context.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Read pump — reads incoming messages from the browser.
	// Handles "response_result" messages for browser-mode custom responses.
	// On disconnect or error, cancels the context to stop the write pump.
	go func() {
		defer cancel()
		for {
			var msg incomingMessage
			err := wsjson.Read(ctx, conn, &msg)
			if err != nil {
				return
			}

			switch msg.Type {
			case "response_result":
				var result hub.ResponseResult
				if err := json.Unmarshal(msg.Data, &result); err != nil {
					ws.log.Warn().Err(err).Str("slug", slug).Msg("invalid response_result message")
					continue
				}
				if result.RequestID == "" {
					ws.log.Warn().Str("slug", slug).Msg("response_result missing request_id")
					continue
				}
				delivered := ws.hub.DeliverResponse(result.RequestID, result)
				if !delivered {
					ws.log.Debug().Str("slug", slug).Str("request_id", result.RequestID).Msg("response_result: no waiting handler (timed out?)")
				} else {
					ws.log.Debug().Str("slug", slug).Str("request_id", result.RequestID).Msg("browser response delivered")
				}
			default:
				// Ignore unknown message types.
			}
		}
	}()

	// Keep-alive: ping every 30s. Uses the derived context so it stops
	// as soon as the connection is done.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.Ping(ctx); err != nil {
					return
				}
			}
		}
	}()

	// Write pump — forward hub messages to WebSocket.
	for {
		select {
		case <-ctx.Done():
			ws.log.Debug().Str("slug", slug).Msg("client disconnected")
			conn.Close(websocket.StatusNormalClosure, "bye")
			return
		case msg, ok := <-ch:
			if !ok {
				ws.log.Debug().Str("slug", slug).Msg("hub channel closed")
				conn.Close(websocket.StatusNormalClosure, "bye")
				return
			}
			// Use a fresh timeout context derived from our connection context.
			// If the connection is already closed, ctx.Done() fires and we
			// skip the write — avoiding the "closed network connection" error.
			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, msg)
			writeCancel()
			if err != nil {
				ws.log.Debug().Err(err).Str("slug", slug).Msg("write failed, closing")
				return
			}
		}
	}
}
