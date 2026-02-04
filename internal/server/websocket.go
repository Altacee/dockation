package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// Client represents a WebSocket client
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub maintains active WebSocket connections
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	logger     *observability.Logger
	running    bool
}

// NewHub creates a new WebSocket hub
func NewHub(logger *observability.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()

	h.logger.Info("websocket hub started")

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Info("websocket client registered",
				zap.Int("total_clients", len(h.clients)),
			)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("websocket client unregistered",
				zap.Int("total_clients", len(h.clients)),
			)

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client send buffer is full, disconnect
					h.mu.RUnlock()
					h.unregister <- client
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Stop stops the hub
func (h *Hub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	h.running = false
	for client := range h.clients {
		close(client.send)
	}
	h.clients = make(map[*Client]bool)

	h.logger.Info("websocket hub stopped")
}

// Broadcast sends a message to all connected clients
func (h *Hub) Broadcast(message []byte) {
	if !h.running {
		return
	}

	select {
	case h.broadcast <- message:
	default:
		h.logger.Warn("broadcast channel full, dropping message")
	}
}

// BroadcastEvent sends a typed event to all clients
func (h *Hub) BroadcastEvent(eventType string, data interface{}) {
	event := map[string]interface{}{
		"type":      eventType,
		"data":      data,
		"timestamp": time.Now().Unix(),
	}

	message, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("failed to marshal event", zap.Error(err))
		return
	}

	h.Broadcast(message)
}

// HandleWebSocket handles WebSocket connection upgrades
func (s *Server) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket", zap.Error(err))
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	client.hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 8192
)

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("websocket read error", zap.Error(err))
			}
			break
		}

		// Reset read deadline on any message received
		c.conn.SetReadDeadline(time.Now().Add(pongWait))

		// Handle incoming messages from client
		c.handleMessage(message)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming messages from clients
func (c *Client) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		c.hub.logger.Warn("failed to unmarshal websocket message", zap.Error(err))
		return
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "ping":
		// Respond with pong
		response := map[string]interface{}{
			"type":      "pong",
			"timestamp": time.Now().Unix(),
		}
		data, _ := json.Marshal(response)
		c.send <- data

	case "subscribe":
		// Handle subscription to specific event types
		// TODO: Implement selective event subscription

	default:
		c.hub.logger.Debug("unknown websocket message type",
			zap.String("type", msgType),
		)
	}
}

// LogStreamClient represents a dedicated log streaming connection
type LogStreamClient struct {
	conn        *websocket.Conn
	containerID string
	done        chan struct{}
}

// HandleContainerLogs streams container logs over WebSocket
func (s *Server) HandleContainerLogs(c *gin.Context) {
	containerID := c.Param("id")
	if containerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "container ID required"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket for logs", zap.Error(err))
		return
	}

	client := &LogStreamClient{
		conn:        conn,
		containerID: containerID,
		done:        make(chan struct{}),
	}

	s.logger.Info("log stream started", zap.String("container_id", containerID))

	// Handle client disconnect
	go func() {
		defer close(client.done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Stream logs
	go s.streamContainerLogs(client)
}

func (s *Server) streamContainerLogs(client *LogStreamClient) {
	defer client.conn.Close()

	// Use background context - the done channel handles cancellation
	ctx := context.Background()

	// Get log stream with follow=true
	reader, err := s.docker.GetContainerLogs(ctx, client.containerID, "100", true)
	if err != nil {
		s.logger.Error("failed to get container logs", zap.Error(err))
		errMsg, _ := json.Marshal(map[string]interface{}{
			"type":  "error",
			"error": err.Error(),
		})
		client.conn.WriteMessage(websocket.TextMessage, errMsg)
		return
	}
	defer reader.Close()

	buf := make([]byte, 8192)
	for {
		select {
		case <-client.done:
			return
		default:
			n, err := reader.Read(buf)
			if err != nil {
				if err.Error() != "EOF" {
					s.logger.Debug("log stream ended", zap.String("container_id", client.containerID))
				}
				return
			}

			if n > 0 {
				// Strip Docker log headers and send
				logData := stripDockerLogHeader(buf[:n])
				if len(logData) > 0 {
					msg, _ := json.Marshal(map[string]interface{}{
						"type": "log",
						"data": string(logData),
					})
					if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				}
			}
		}
	}
}

// stripDockerLogHeader removes Docker's multiplexed stream headers
func stripDockerLogHeader(data []byte) []byte {
	var result []byte
	for len(data) >= 8 {
		// Docker stream header: [stream_type:1][0:3][size:4]
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		if size <= 0 || 8+size > len(data) {
			// Invalid header, return remaining data as-is
			result = append(result, data...)
			break
		}
		result = append(result, data[8:8+size]...)
		data = data[8+size:]
	}
	if len(data) > 0 && len(result) == 0 {
		return data
	}
	return result
}
