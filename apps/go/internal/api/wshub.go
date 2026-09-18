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
	// Recheck sessions before sending updates, including across API replicas.
	authorizeUser func(userID, tokenVersion int) bool
}

// Client wraps a single WebSocket connection together with the application scope of the
// user that opened it, so a connection only receives updates it is allowed to see.
type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	scope        applicationScope
	userID       int
	tokenVersion int
	expiresAt    time.Time
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

// DisconnectUser ends existing live streams when an account's access changes.
func (h *Hub) DisconnectUser(userID int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.userID == userID {
			delete(h.clients, c)
			close(c.send)
			if c.conn != nil {
				_ = c.conn.Close()
			}
		}
	}
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
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	type session struct{ userID, tokenVersion int }
	validated := make(map[session]bool)
	for _, c := range clients {
		if !time.Now().Before(c.expiresAt) {
			h.unregister(c)
			if c.conn != nil {
				_ = c.conn.Close()
			}
			continue
		}
		if !c.scope.allows(envelope.ApplicationID) {
			continue
		}
		if h.authorizeUser != nil {
			key := session{c.userID, c.tokenVersion}
			valid, checked := validated[key]
			if !checked {
				valid = h.authorizeUser(c.userID, c.tokenVersion)
				validated[key] = valid
			}
			if !valid {
				h.unregister(c)
				if c.conn != nil {
					_ = c.conn.Close()
				}
				continue
			}
		}
		h.mu.RLock()
		if _, connected := h.clients[c]; !connected || !time.Now().Before(c.expiresAt) {
			h.mu.RUnlock()
			continue
		}
		select {
		case c.send <- msg:
		default:
			// Client too slow, drop message to avoid blocking.
		}
		h.mu.RUnlock()
	}
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	expiry := time.NewTimer(time.Until(c.expiresAt))
	defer func() {
		ticker.Stop()
		expiry.Stop()
		c.hub.unregister(c)
		c.conn.Close()
	}()

	for {
		select {
		case <-expiry.C:
			return
		case msg, ok := <-c.send:
			if !time.Now().Before(c.expiresAt) {
				return
			}
			_ = c.conn.SetWriteDeadline(c.writeDeadline())
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if !time.Now().Before(c.expiresAt) {
				return
			}
			_ = c.conn.SetWriteDeadline(c.writeDeadline())
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) writeDeadline() time.Time {
	deadline := time.Now().Add(writeWait)
	if c.expiresAt.Before(deadline) {
		return c.expiresAt
	}
	return deadline
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
	expiresAt, ok := r.Context().Value(tokenExpiresAtKey).(time.Time)
	if !ok || !time.Now().Before(expiresAt) {
		writeJSONError(w, "session expired", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("ws: upgrade failed", "err", err)
		return
	}

	client := &Client{
		hub:       h,
		conn:      conn,
		send:      make(chan []byte, 256),
		scope:     scope,
		userID:    getUserIDFromContext(r.Context()),
		expiresAt: expiresAt,
	}
	client.tokenVersion, _ = r.Context().Value(tokenVersionKey).(int)
	h.register(client)

	go client.writePump()
	go client.readPump()
}
