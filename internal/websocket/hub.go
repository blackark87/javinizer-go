package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// ProgressMessage represents a progress update message
type ProgressMessage struct {
	JobID     string  `json:"job_id"`
	FileIndex int     `json:"file_index"`
	FilePath  string  `json:"file_path"`
	Status    string  `json:"status"`
	Progress  float64 `json:"progress"`
	Message   string  `json:"message"`
	Error     string  `json:"error,omitempty"`
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// Client represents a websocket client
type Client struct {
	conn       *websocket.Conn
	send       chan []byte
	remoteAddr string
	origin     string
	host       string
}

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.RWMutex
}

// NewHub creates a new Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.closeDone()

			// Context cancelled, clean up all clients
			// Snapshot clients first to minimize lock duration
			h.mu.Lock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.clients = make(map[*Client]bool)
			h.mu.Unlock()

			// Clean up clients without holding lock
			for _, client := range clients {
				close(client.send)
			}
			logging.Infof("WebSocket hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			logging.Infof("WebSocket client connected. Total clients: %d remote_addr=%q origin=%q host=%q", len(h.clients), client.remoteAddr, client.origin, client.host)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			logging.Infof("WebSocket client disconnected. Total clients: %d remote_addr=%q origin=%q host=%q", len(h.clients), client.remoteAddr, client.origin, client.host)

		case message := <-h.broadcast:
			// Phase 1: Collect clients under read lock (prevents deadlock during channel sends)
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clients = append(clients, client)
			}
			h.mu.RUnlock()

			// Phase 2: Send to clients without holding lock (channel sends can block)
			var toRemove []*Client
			for _, client := range clients {
				select {
				case client.send <- message:
					// Message sent successfully
				default:
					// Client's send channel is full, mark for removal
					close(client.send)
					toRemove = append(toRemove, client)
				}
			}

			// Phase 3: Remove disconnected clients with brief write lock
			if len(toRemove) > 0 {
				h.mu.Lock()
				for _, client := range toRemove {
					delete(h.clients, client)
				}
				h.mu.Unlock()
			}
		}
	}
}

func (h *Hub) closeDone() {
	h.closeOnce.Do(func() {
		close(h.done)
	})
}

// Register registers a new client
func (h *Hub) Register(client *Client) {
	if h == nil || client == nil {
		return
	}
	select {
	case <-h.done:
		return
	case h.register <- client:
	}
}

// Unregister unregisters a client
func (h *Hub) Unregister(client *Client) {
	if h == nil || client == nil {
		return
	}
	select {
	case <-h.done:
		return
	case h.unregister <- client:
	}
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(message interface{}) error {
	// Handle nil hub (can occur during cleanup in tests when hub is being replaced)
	if h == nil {
		return nil // Silently ignore broadcasts to nil hub
	}

	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	// Use select with default to avoid blocking if hub is shutting down
	select {
	case h.broadcast <- data:
		return nil
	default:
		// Hub is busy or shutting down, drop the message to avoid blocking
		return nil
	}
}

// BroadcastProgress sends a progress update to all clients
func (h *Hub) BroadcastProgress(msg *ProgressMessage) error {
	return h.Broadcast(msg)
}

// NewClient creates a new client.
func NewClient(conn *websocket.Conn, requests ...*http.Request) *Client {
	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}
	if len(requests) > 0 && requests[0] != nil {
		r := requests[0]
		client.remoteAddr = r.RemoteAddr
		client.origin = r.Header.Get("Origin")
		client.host = r.Host
	}
	return client
}

// WritePump pumps messages from the hub to the websocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				logging.Errorf("Error writing to websocket: %v remote_addr=%q origin=%q host=%q", err, c.remoteAddr, c.origin, c.host)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				logging.Errorf("Error writing websocket ping: %v remote_addr=%q origin=%q host=%q", err, c.remoteAddr, c.origin, c.host)
				return
			}
		}
	}
}

// ReadPump pumps messages from the websocket connection to the hub.
func (c *Client) ReadPump(hub *Hub) {
	defer func() {
		hub.Unregister(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			c.logReadError(err)
			break
		}
		// We don't process client messages for now, just keep the connection alive.
	}
}

func (c *Client) logReadError(err error) {
	if closeErr, ok := err.(*websocket.CloseError); ok {
		format := "WebSocket closed code=%d reason=%q remote_addr=%q origin=%q host=%q"
		if isExpectedCloseCode(closeErr.Code) {
			logging.Infof(format, closeErr.Code, closeErr.Text, c.remoteAddr, c.origin, c.host)
			return
		}
		logging.Warnf(format, closeErr.Code, closeErr.Text, c.remoteAddr, c.origin, c.host)
		return
	}

	logging.Errorf("WebSocket read error: %v remote_addr=%q origin=%q host=%q", err, c.remoteAddr, c.origin, c.host)
}

func isExpectedCloseCode(code int) bool {
	switch code {
	case websocket.CloseNormalClosure, websocket.CloseGoingAway:
		return true
	default:
		return false
	}
}
