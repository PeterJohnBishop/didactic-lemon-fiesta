package relayclient

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"net"
	"os"
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
	// clientID, err := GenerateID(8)
	clientID := os.Getenv("CLIENT_ID")
	targetID := os.Getenv("TARGET_ID")
	filepathEnv := os.Getenv("FILE")
	var data *ChunkMetadata
	var err error
	if filepathEnv != "" {
		data, err = splitFile(filepathEnv)
		if err != nil {
			panic(err)
		}
		fmt.Printf(data.FileName)
	}
	// dial server
	conn, err := net.Dial("tcp", url)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	fmt.Printf("Registering as %s...\n", clientID)

	conn.Write([]byte{byte(len(clientID))})
	conn.Write([]byte(clientID))

	go func() {
		for {
			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				fmt.Println("Disconnected from server")
				return
			}
			fmt.Printf("\n[RECEIVED]: %s\n", string(buf[:n]))
		}
	}()

	if data != nil {

		for {
			payload := fmt.Sprintf("Metadata: %s (%d chunks)", data.FileName, data.NumChunks)
			// target ID Length
			conn.Write([]byte{byte(len(targetID))})

			// target ID
			conn.Write([]byte(targetID))

			// payload Size
			pSize := uint32(len(payload))
			binary.Write(conn, binary.BigEndian, pSize)

			// payload
			conn.Write([]byte(payload))

			time.Sleep(5 * time.Second)
		}
	} else {

		// test data sending
		message := "File: " + clientID + "!"

		for {
			fmt.Printf("Sending message to %s...\n", targetID)

			// target ID Length
			conn.Write([]byte{byte(len(targetID))})

			// target ID
			conn.Write([]byte(targetID))

			// payload Size
			pSize := uint32(len(message))
			binary.Write(conn, binary.BigEndian, pSize)

			// payload
			conn.Write([]byte(message))

			time.Sleep(5 * time.Second)
		}
	}

}
