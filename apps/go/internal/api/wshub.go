package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Hub manages WebSocket connections and broadcasts messages to all clients.
type Hub struct {
	mu       sync.RWMutex
	clients  map[*Client]struct{}
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

// Client wraps a single WebSocket connection together with the application scope of the
// user that opened it, so a connection only receives updates it is allowed to see.
type Client struct {
	hub   *Hub
	conn  *websocket.Conn
	send  chan []byte
	scope applicationScope
}

func NewHub(logger *slog.Logger, allowedOrigins []string) *Hub {
	allowedOriginSet := allowedOriginsMap(allowedOrigins)
	return &Hub{
		clients: make(map[*Client]struct{}),
		logger:  logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return false
				}
				return isAllowedOrigin(allowedOriginSet, origin)
			},
		},
	}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.logger.Info("ws: client connected", "clients", h.clientCount())
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
	h.logger.Info("ws: client disconnected", "clients", h.clientCount())
}

func (h *Hub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends a pipeline update to the clients whose application scope covers it.
// An update whose owning application cannot be determined is dropped rather than fanned
// out, so a malformed payload can never leak across applications.
func (h *Hub) Broadcast(msg []byte) {
	var envelope struct {
		ApplicationID int `json:"applicationId"`
	}
	if err := json.Unmarshal(msg, &envelope); err != nil || envelope.ApplicationID <= 0 {
		h.logger.Warn("ws: dropping broadcast without a resolvable application")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if !c.scope.allows(envelope.ApplicationID) {
			continue
		}
		select {
		case c.send <- msg:
		default:
			// Client too slow, drop message to avoid blocking.
		}
	}
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// ServeWS handles a WebSocket upgrade request.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	scope := scopeFromContext(r.Context())
	if scope.isEmpty() {
		writeJSONError(w, "no application access", http.StatusForbidden)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("ws: upgrade failed", "err", err)
		return
	}

	client := &Client{
		hub:   h,
		conn:  conn,
		send:  make(chan []byte, 256),
		scope: scope,
	}
	h.register(client)

	go client.writePump()
	go client.readPump()
}
