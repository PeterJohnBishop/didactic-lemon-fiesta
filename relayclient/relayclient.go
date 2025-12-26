package relayclient

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"
)

func LaunchRelayClient() {
	url := os.Getenv("SERVER_URL")
	clientID := os.Getenv("CLIENT_ID")
	targetID := os.Getenv("TARGET_ID")

	if url == "" || clientID == "" || targetID == "" {
		fmt.Println("Please set SERVER_URL, CLIENT_ID, and TARGET_ID environment variables.")
		return
	}

	conn, err := net.Dial("tcp", url)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	myID := clientID
	fmt.Printf("Registering as %s...\n", myID)

	conn.Write([]byte{byte(len(myID))})
	conn.Write([]byte(myID))

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

	// test data sending
	message := "Hello from " + clientID + "!"

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
