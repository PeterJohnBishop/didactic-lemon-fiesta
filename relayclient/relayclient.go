package relayclient

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateID(length int) (string, error) {
	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}
	return string(result), nil
}

func LaunchRelayClient() {
	url := os.Getenv("SERVER_URL")
	secret := os.Getenv("SECRET")
	filepathEnv := os.Getenv("FILE")
	clientID, _ := GenerateID(8)

	if url == "" || secret == "" {
		fmt.Println("Error: SERVER_URL and SECRET are required.")
		return
	}

	var metadata *ChunkMetadata
	if filepathEnv != "" {
		var err error
		metadata, err = splitFile(filepathEnv)
		if err != nil {
			fmt.Printf("Error splitting file: %v\n", err)
			return
		}
	}

	conn, err := net.Dial("tcp", url)
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		return
	}
	defer conn.Close()

	// 1. Handshake
	fmt.Printf("Registering as %s with secret...\n", clientID)
	conn.Write([]byte{byte(len(clientID))})
	conn.Write([]byte(clientID))
	conn.Write([]byte{byte(len(secret))})
	conn.Write([]byte(secret))

	connectedSignal := make(chan string, 1)

	go func() {
		defer close(connectedSignal)

		for {
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				fmt.Printf("\n[ERROR] Connection closed: %v\n", err)
				return
			}
			msg := string(buf[:n])
			fmt.Printf("\n[RECEIVED]: %s\n", msg)

			if strings.HasPrefix(msg, "CONNECTED:") {
				peerID := strings.TrimPrefix(msg, "CONNECTED:")
				connectedSignal <- peerID
			}
		}
	}()

	fmt.Println("Awaiting peer match via secret...")

	peerID, ok := <-connectedSignal
	if !ok {
		fmt.Println("Exiting: Connection to relay was lost before matching occurred.")
		return
	}
	fmt.Printf("Successfully matched with peer: %s\n", peerID)

	if metadata != nil {
		metaJSON, _ := json.Marshal(metadata)
		sendPayload(conn, metaJSON)
		fmt.Printf("Sent metadata for: %s\n", metadata.FileName)
	}

	for {
		binary.Write(conn, binary.BigEndian, uint32(0))
		time.Sleep(20 * time.Second)
	}
}

func sendPayload(conn net.Conn, data []byte) {
	pSize := uint32(len(data))
	binary.Write(conn, binary.BigEndian, pSize)
	conn.Write(data)
}
