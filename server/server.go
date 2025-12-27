package server

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
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
	port := os.Getenv("SERVER_PORT")
	hub := &Hub{
		clients: make(map[string]*Client),
		secrets: make(map[string]string),
	}
	listener, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Println("Error starting server:", err)
		return
	}

	fmt.Println("Relay server started on :8080")
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go hub.handleWithHeartbeat(conn)
	}
}

func (c *Client) writeLoop() {
	for msg := range c.Outgoing {
		c.Writer.Write(msg)
		c.Writer.Flush()
	}
}

func (h *Hub) handleWithHeartbeat(conn net.Conn) {
	defer conn.Close()

	idLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, idLenBuf); err != nil {
		return
	}
	idBuf := make([]byte, idLenBuf[0])
	io.ReadFull(conn, idBuf)
	clientID := string(idBuf)

	secretLenBuf := make([]byte, 1)
	io.ReadFull(conn, secretLenBuf)
	secretBuf := make([]byte, secretLenBuf[0])
	io.ReadFull(conn, secretBuf)
	secret := string(secretBuf)

	client := &Client{
		ID:       clientID,
		Secret:   secret,
		Conn:     conn,
		Writer:   bufio.NewWriterSize(conn, 4096),
		Outgoing: make(chan []byte, 100),
	}

	h.mu.Lock()

	peerID, matched := h.secrets[secret]

	if matched && peerID != client.ID {
		peer := h.clients[peerID]

		client.TargetID = peer.ID
		peer.TargetID = client.ID

		delete(h.secrets, secret)
		h.mu.Unlock()

		client.Outgoing <- []byte("CONNECTED:" + peer.ID)
		peer.Outgoing <- []byte("CONNECTED:" + client.ID)

		go client.writeLoop()
		h.runRelayLoop(client)
		return
	}

	h.clients[client.ID] = client
	h.secrets[secret] = client.ID
	h.mu.Unlock()

	go client.writeLoop()

	defer func() {
		h.mu.Lock()
		delete(h.clients, client.ID)
		delete(h.secrets, client.Secret)
		h.mu.Unlock()
		close(client.Outgoing)
	}()

	h.runRelayLoop(client)
}

func (h *Hub) runRelayLoop(client *Client) {
	for {
		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var pSize uint32
		if err := binary.Read(client.Conn, binary.BigEndian, &pSize); err != nil {
			break
		}

		payload := make([]byte, pSize)
		if _, err := io.ReadFull(client.Conn, payload); err != nil {
			break
		}

		h.mu.RLock()
		targetID := client.TargetID

		if targetID == "" {
			// recheck the client in the hub
			if self, exists := h.clients[client.ID]; exists {
				targetID = self.TargetID
			}
		}
		h.mu.RUnlock()

		if targetID != "" {
			h.forward(targetID, payload)
		} else {
			fmt.Printf("Client %s is sending data but has no target yet\n", client.ID)
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
			fmt.Printf("Outgoing buffer full for %s, dropping packet\n", targetID)
		}
	}
}
