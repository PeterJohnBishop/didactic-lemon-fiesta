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

// writeLoop pulls data from the Outgoing channel sends it!
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

// manages the iregistration and matchmaking
func (h *Hub) handleWithHeartbeat(conn net.Conn) {
	// read Client ID
	idLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, idLenBuf); err != nil {
		conn.Close()
		return
	}
	idBuf := make([]byte, idLenBuf[0])
	io.ReadFull(conn, idBuf)
	clientID := string(idBuf)

	// read Secret
	secretLenBuf := make([]byte, 1)
	io.ReadFull(conn, secretLenBuf)
	secretBuf := make([]byte, secretLenBuf[0])
	io.ReadFull(conn, secretBuf)
	secret := string(secretBuf)

	client := &Client{
		ID:       clientID,
		Secret:   secret,
		Conn:     conn,
		Writer:   bufio.NewWriterSize(conn, 32*1024),
		Outgoing: make(chan []byte, 256),
	}

	fmt.Printf("[SYSTEM] connection: %s (secret: %s)\n", client.ID, client.Secret)

	h.mu.Lock()
	h.clients[client.ID] = client

	peerID, matched := h.secrets[secret]

	if matched && peerID != client.ID {
		if peer, exists := h.clients[peerID]; exists {
			fmt.Printf("[MATCH] %s <> %s linked\n", client.ID, peer.ID)

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

	// no peer yet, register the secret and wait
	h.secrets[secret] = client.ID
	h.mu.Unlock()

	go client.writeLoop()

	h.runRelayLoop(client)
	h.cleanup(client.ID)
}

// handles incoming data and forwards data to the target.
func (h *Hub) runRelayLoop(client *Client) {
	defer client.Conn.Close()

	for {
		var pSize uint32
		// read 4-byte size
		err := binary.Read(client.Conn, binary.BigEndian, &pSize)
		if err != nil {
			fmt.Printf("[SYSTEM] %s disconnected\n", client.ID)
			break
		}

		if pSize == 0 {
			// it's a ping to keep the connection alive
			continue
		}

		// Read the payload
		payload := make([]byte, pSize)
		if _, err := io.ReadFull(client.Conn, payload); err != nil {
			fmt.Printf("[ERROR] readFull failed for %s\n", client.ID)
			break
		}

		h.mu.RLock()
		targetID := client.TargetID
		h.mu.RUnlock()

		if targetID != "" {
			fullPacket := make([]byte, 4+pSize)
			binary.BigEndian.PutUint32(fullPacket[0:4], pSize)
			copy(fullPacket[4:], payload)

			h.forward(targetID, fullPacket)
		} else {
			// when Client A sends data before Client B connects
			fmt.Printf("[DEBUG] dropped %d bytes from %s: No peer matched yet\n", pSize, client.ID)
		}
	}
}

// forward safely delivers a payload to a target client outgoing chan
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
	} else {
		fmt.Printf("[ERROR] Forward failed: Target %s not in client map\n", targetID)
	}
}

// wraps raw bytes in the 4-byte size header and queues for sending for messages
func (c *Client) sendProtocolMsg(data []byte) {
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(data)))
	copy(buf[4:], data)

	select {
	case c.Outgoing <- buf:
	default:
	}
}

// removes the client and their associated secret on disconnect
func (h *Hub) cleanup(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client, exists := h.clients[id]
	if exists {
		for sec, cid := range h.secrets {
			if cid == id {
				delete(h.secrets, sec)
			}
		}
		delete(h.clients, id)
		close(client.Outgoing)
		fmt.Printf("[SYSTEM] cleaned up client %s\n", id)
	}
}
