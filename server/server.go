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
	Conn     net.Conn
	Writer   *bufio.Writer
	Outgoing chan []byte // Channel to handle outgoing data asynchronously
}

type Hub struct {
	clients map[string]*Client
	mu      sync.RWMutex
}

func LaunchRelayServer() {
	port := os.Getenv("SERVER_PORT")
	hub := &Hub{clients: make(map[string]*Client)}
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

// goroutines for each client to handle outgoing messages
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
	if _, err := io.ReadFull(conn, idBuf); err != nil {
		return
	}

	client := &Client{
		ID:       string(idBuf),
		Conn:     conn,
		Writer:   bufio.NewWriterSize(conn, 4096),
		Outgoing: make(chan []byte, 100), // Buffer up to 100 messages
	}

	h.mu.Lock()
	h.clients[client.ID] = client
	h.mu.Unlock()

	go client.writeLoop() // writeLoop for outgoing messages starts

	defer func() {
		h.mu.Lock()
		delete(h.clients, client.ID)
		h.mu.Unlock()
		close(client.Outgoing) // writeLoop stops
		fmt.Printf("Client %s disconnected\n", client.ID)
	}()

	fmt.Printf("Client %s registered\n", client.ID)

	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		// Read Target ID Length
		if _, err := io.ReadFull(conn, idLenBuf); err != nil {
			break
		}

		// Read Target ID
		tIDBuf := make([]byte, idLenBuf[0])
		if _, err := io.ReadFull(conn, tIDBuf); err != nil {
			break
		}
		targetID := string(tIDBuf)

		// Read Payload Size
		var pSize uint32
		if err := binary.Read(conn, binary.BigEndian, &pSize); err != nil {
			break
		}

		// Read Payload
		payload := make([]byte, pSize)
		if _, err := io.ReadFull(conn, payload); err != nil {
			break
		}

		h.forward(targetID, payload)
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
