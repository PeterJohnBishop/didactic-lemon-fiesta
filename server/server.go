package server

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
)

type Client struct {
	ID       string
	Secret   string
	TargetID string
	Conn     net.Conn
	Writer   *bufio.Writer
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

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Printf("[ERROR] failed to start server: %v\n", err)
		return
	}

	fmt.Printf("[SYSTEM] relay server active on :%s\n", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("[ERROR] Accept error: %v\n", err)
			continue
		}
		go hub.handleWithHeartbeat(conn)
	}
}

func (c *Client) writeLoop() {
	defer fmt.Printf("[DEBUG] %s exited writeloop\n", c.ID)
	for msg := range c.Outgoing {
		if c.Conn == nil {
			return
		}
		_, err := c.Writer.Write(msg)
		if err != nil {
			fmt.Printf("[ERROR] write failed for %s: %v\n", c.ID, err)
			return
		}
		c.Writer.Flush()
	}
}

func (h *Hub) handleWithHeartbeat(conn net.Conn) {
	fmt.Printf("[DEBUG] New raw connection from %s\n", conn.RemoteAddr())

	// 1. Read Client ID Length
	idLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, idLenBuf); err != nil {
		fmt.Printf("[DEBUG] Failed to read ID length: %v\n", err)
		conn.Close()
		return
	}

	// 2. Read Client ID
	idBuf := make([]byte, idLenBuf[0])
	if _, err := io.ReadFull(conn, idBuf); err != nil {
		fmt.Printf("[DEBUG] Failed to read ID body: %v\n", err)
		conn.Close()
		return
	}
	clientID := string(idBuf)

	// 3. Read Secret Length
	secretLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, secretLenBuf); err != nil {
		fmt.Printf("[DEBUG] Failed to read Secret length: %v\n", err)
		conn.Close()
		return
	}

	// 4. Read Secret
	secretBuf := make([]byte, secretLenBuf[0])
	if _, err := io.ReadFull(conn, secretBuf); err != nil {
		fmt.Printf("[DEBUG] Failed to read Secret body: %v\n", err)
		conn.Close()
		return
	}
	secret := string(secretBuf)

	client := &Client{
		ID:       clientID,
		Secret:   secret,
		Conn:     conn,
		Writer:   bufio.NewWriterSize(conn, 32*1024),
		Outgoing: make(chan []byte, 256),
	}

	fmt.Printf("[SYSTEM] Authenticated: %s (Secret: %s)\n", client.ID, client.Secret)

	h.mu.Lock()
	h.clients[client.ID] = client
	peerID, matched := h.secrets[secret]

	if matched && peerID != client.ID {
		if peer, exists := h.clients[peerID]; exists {
			fmt.Printf("[MATCH] %s <> %s linked on secret %s\n", client.ID, peer.ID, secret)

			client.TargetID = peer.ID
			peer.TargetID = client.ID

			delete(h.secrets, secret)
			h.mu.Unlock()

			// Start writing goroutines for both
			go client.writeLoop()
			go peer.writeLoop()

			// Alert both clients
			client.sendProtocolMsg([]byte("CONNECTED:" + peer.ID))
			peer.sendProtocolMsg([]byte("CONNECTED:" + client.ID))

			// Run relay for this client (blocks this handler)
			h.runRelayLoop(client)
			h.cleanup(client.ID)
			return
		}
	}

	// No peer found, register secret and wait
	h.secrets[secret] = client.ID
	h.mu.Unlock()

	fmt.Printf("[SYSTEM] %s waiting for peer with secret %s...\n", client.ID, secret)
	go client.writeLoop()

	h.runRelayLoop(client)
	h.cleanup(client.ID)
}

func (h *Hub) runRelayLoop(client *Client) {
	defer client.Conn.Close()

	for {
		var pSize uint32
		err := binary.Read(client.Conn, binary.BigEndian, &pSize)
		if err != nil {
			// Expected on disconnect
			break
		}

		if pSize == 0 {
			continue // Keep-alive
		}

		payload := make([]byte, pSize)
		if _, err := io.ReadFull(client.Conn, payload); err != nil {
			fmt.Printf("[ERROR] failed to read payload for %s: %v\n", client.ID, err)
			break
		}

		h.mu.RLock()
		targetID := client.TargetID
		h.mu.RUnlock()

		if targetID != "" {
			// Prepend size and forward
			fullPacket := make([]byte, 4+pSize)
			binary.BigEndian.PutUint32(fullPacket[0:4], pSize)
			copy(fullPacket[4:], payload)

			h.forward(targetID, fullPacket)
		} else {
			fmt.Printf("[DEBUG] dropped %d bytes from %s: No peer\n", pSize, client.ID)
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
			fmt.Printf("[WARNING] buffer full for %s, dropping packet\n", targetID)
		}
	}
}

func (c *Client) sendProtocolMsg(data []byte) {
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(data)))
	copy(buf[4:], data)

	select {
	case c.Outgoing <- buf:
	default:
	}
}

func (h *Hub) cleanup(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client, exists := h.clients[id]
	if exists {
		// Clean up secrets associated with this ID
		for sec, cid := range h.secrets {
			if cid == id {
				delete(h.secrets, sec)
			}
		}
		delete(h.clients, id)

		// Signal Target that peer is gone
		if client.TargetID != "" {
			if target, ok := h.clients[client.TargetID]; ok {
				target.TargetID = ""
			}
		}

		close(client.Outgoing)
		fmt.Printf("[SYSTEM] cleaned up client %s\n", id)
	}
}
