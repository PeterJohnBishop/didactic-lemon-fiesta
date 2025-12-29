package server

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	ID       string
	Secret   string
	TargetID string
	Conn     *websocket.Conn
	Outgoing chan []byte
}

type Hub struct {
	secrets map[string]string
	clients map[string]*Client
	mu      sync.RWMutex
}

func LaunchRelayServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hub := &Hub{
		clients: make(map[string]*Client),
		secrets: make(map[string]string),
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			fmt.Printf("[ERROR] Upgrade failed: %v\n", err)
			return
		}
		go hub.handleWebSocket(wsConn)
	})

	fmt.Printf("[SYSTEM] WebSocket Relay active on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Printf("[ERROR] Server failed: %v\n", err)
	}
}

func (h *Hub) handleWebSocket(conn *websocket.Conn) {
	_, message, err := conn.ReadMessage()
	if err != nil || len(message) < 2 {
		conn.Close()
		return
	}

	idLen := int(message[0])
	clientID := string(message[1 : 1+idLen])
	secLen := int(message[1+idLen])
	secret := string(message[2+idLen : 2+idLen+secLen])

	client := &Client{
		ID:       clientID,
		Secret:   secret,
		Conn:     conn,
		Outgoing: make(chan []byte, 256),
	}

	fmt.Printf("[SYSTEM] Authenticated: %s (Secret: %s)\n", clientID, secret)

	h.mu.Lock()
	h.clients[clientID] = client
	peerID, matched := h.secrets[secret]

	if matched && peerID != clientID {
		if peer, exists := h.clients[peerID]; exists {
			client.TargetID = peer.ID
			peer.TargetID = client.ID
			delete(h.secrets, secret)
			h.mu.Unlock()

			go client.writeLoop()
			go peer.writeLoop()

			client.sendProtocolMsg([]byte("CONNECTED:" + peer.ID))
			peer.sendProtocolMsg([]byte("CONNECTED:" + client.ID))

			h.runRelayLoop(client)
			h.cleanup(client.ID)
			return
		}
	}

	h.secrets[secret] = clientID
	h.mu.Unlock()

	go client.writeLoop()
	h.runRelayLoop(client)
	h.cleanup(client.ID)
}

func (h *Hub) runRelayLoop(client *Client) {
	defer client.Conn.Close()
	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		h.mu.RLock()
		targetID := client.TargetID
		h.mu.RUnlock()

		if targetID != "" {
			h.forward(targetID, message)
		}
	}
}

func (h *Hub) forward(targetID string, payload []byte) {
	h.mu.RLock()
	target, exists := h.clients[targetID]
	h.mu.RUnlock()

	if exists {
		select {
		case target.Outgoing <- payload:
		default:
		}
	}
}

func (c *Client) writeLoop() {
	for msg := range c.Outgoing {
		if err := c.Conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			return
		}
	}
}

func (c *Client) sendProtocolMsg(data []byte) {
	select {
	case c.Outgoing <- data:
	default:
	}
}

func (h *Hub) cleanup(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if client, exists := h.clients[id]; exists {
		for sec, cid := range h.secrets {
			if cid == id {
				delete(h.secrets, sec)
			}
		}
		delete(h.clients, id)
		close(client.Outgoing)
		fmt.Printf("[SYSTEM] Cleaned up %s\n", id)
	}
}
