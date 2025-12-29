package relayclient

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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

var activeDownloadMeta *ChunkMetadata
var filesMetadata []ChunkMetadata
var dir string
var writeMu sync.Mutex

func LaunchRelayClient() {
	os.MkdirAll("temp_chunks", os.ModePerm)
	os.MkdirAll("chunks", os.ModePerm)

	secret := os.Getenv("SECRET")
	clientID, _ := GenerateID(8)

	if secret == "" {
		fmt.Println("[ERROR] SECRET is required")
		return
	}

	currentUser, err := user.Current()
	if err != nil {
		log.Fatalf("Failed to get current user: %v", err)
	}
	dir = "/Users/" + currentUser.Username + "/Downloads"

	filesMetadata, err = scanForFiles(dir)
	if err != nil {
		log.Fatalf("Failed to scan for files: %v", err)
	}

	u := url.URL{
		Scheme: "wss",
		Host:   "relaysvr-didactic-lemon-fiesta-8fd835c555af.herokuapp.com",
		Path:   "/",
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	fmt.Printf("[SYSTEM] Dialing %s...\n", u.String())
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			fmt.Printf("[ERROR] Handshake failed with status: %s\n", resp.Status)
		}
		log.Fatalf("[ERROR] WebSocket Dial: %v", err)
	}
	defer conn.Close()

	fmt.Println("[SYSTEM] Connected securely to Heroku Relay via WebSocket!")

	fmt.Printf("[SYSTEM] Registering as %s...\n", clientID)
	regBuf := []byte{}
	regBuf = append(regBuf, byte(len(clientID)))
	regBuf = append(regBuf, []byte(clientID)...)
	regBuf = append(regBuf, byte(len(secret)))
	regBuf = append(regBuf, []byte(secret)...)

	writeMu.Lock()
	err = conn.WriteMessage(websocket.BinaryMessage, regBuf)
	writeMu.Unlock()
	if err != nil {
		fmt.Printf("[ERROR] Registration write failed: %v\n", err)
		return
	}

	connectedSignal := make(chan string, 1)

	go func() {
		fmt.Println("[DEBUG] Listener goroutine started")
		defer close(connectedSignal)
		var peerID string

		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("[SYSTEM] Connection closed: %v\n", err)
				return
			}

			if len(payload) == 0 {
				continue
			}

			dataStr := string(payload)
			headerByte := payload[0]

			switch headerByte {
			case 0xAA:
				if len(payload) < 5 {
					continue
				}
				index := binary.BigEndian.Uint32(payload[1:5])
				fmt.Printf("[LOG] Received chunk index %d (%d bytes)\n", index, len(payload)-5)

				if activeDownloadMeta != nil {
					saveChunk(activeDownloadMeta.FileName, index, payload[5:])
					checkProgress(activeDownloadMeta)
				}
				continue

			case '{':
				var generic map[string]interface{}
				if err := json.Unmarshal(payload, &generic); err != nil {
					continue
				}

				if _, ok := generic["file_name"]; ok {
					var incomingMeta ChunkMetadata
					if err := json.Unmarshal(payload, &incomingMeta); err == nil {
						fmt.Printf("[RECEIVER] Manifest: %s\n", incomingMeta.FileName)
						activeDownloadMeta = &incomingMeta

						checkProgress(activeDownloadMeta)

						go handleIncomingMetadata(conn, incomingMeta)
					}
					continue
				}

				if val, ok := generic["type"].(string); ok && val == "CHUNK_REQ" {
					fileName, okF := generic["file"].(string)
					indexFloat, okI := generic["index"].(float64)

					if okF && okI {
						idx := int(indexFloat)
						chunkPath := filepath.Join("chunks", fmt.Sprintf("%s.chunk.%d", fileName, idx))
						chunkData, err := os.ReadFile(chunkPath)
						if err == nil {
							sendPayload(conn, wrapChunk(idx, chunkData))
							fmt.Printf("[SENDER] Sent chunk %d for %s\n", idx, fileName)
						}
					}
					continue
				}

			case 'C':
				if strings.HasPrefix(dataStr, "CONNECTED:") {
					peerID = strings.TrimPrefix(dataStr, "CONNECTED:")
					connectedSignal <- peerID
					continue
				}
			}
		}
	}()

	fmt.Println("[SYSTEM] Awaiting client connection...")
	_, ok := <-connectedSignal
	if !ok {
		return
	}
	fmt.Println("[SYSTEM] Peer matched! Starting sync...")

	for _, metadata := range filesMetadata {
		metaJSON, _ := json.Marshal(metadata)
		sendPayload(conn, metaJSON)
		fmt.Printf("[SENDER] Metadata pushed: %s\n", metadata.FileName)
		time.Sleep(200 * time.Millisecond)
	}

	// Keep-alive Heartbeat loop
	for {
		writeMu.Lock()
		// Send Ping every 20s to stay inside Heroku's 30s timeout
		err := conn.WriteMessage(websocket.PingMessage, nil)
		writeMu.Unlock()
		if err != nil {
			fmt.Printf("[SYSTEM] Heartbeat lost: %v\n", err)
			return
		}
		time.Sleep(20 * time.Second)
	}
}

func scanForFiles(dir string) ([]ChunkMetadata, error) {
	files, err := GetAllFiles(dir)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d files in %s and subdirectories\n", len(files), dir)
	var allMetadata []ChunkMetadata
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		modTime := info.ModTime()

		metadata, err := splitFile(file, modTime)
		if err != nil {
			fmt.Printf("[ERROR] split failed: %v\n", err)
			continue
		}
		allMetadata = append(allMetadata, *metadata)
	}
	return allMetadata, nil
}

func handleIncomingMetadata(conn *websocket.Conn, meta ChunkMetadata) {
	fmt.Printf("[RECEIVER] Syncing %s (%d chunks)\n", meta.FileName, meta.NumChunks)

	time.Sleep(1 * time.Second)

	for i, hash := range meta.ChunkHashes {
		chunkPath := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", meta.FileName, i))

		if info, err := os.Stat(chunkPath); err == nil {
			if info.Size() > 0 {
				continue
			}
		}

		request := map[string]interface{}{
			"type":  "CHUNK_REQ",
			"index": i,
			"hash":  hash,
			"file":  meta.FileName,
		}
		reqBytes, _ := json.Marshal(request)
		sendPayload(conn, reqBytes)

		if i > 0 && i%50 == 0 {
			fmt.Printf("\n[DEBUG] Batch pause at chunk %d to clear buffers...\n", i)
			time.Sleep(1500 * time.Millisecond)
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func sendPayload(conn *websocket.Conn, data []byte) {
	writeMu.Lock()
	defer writeMu.Unlock()
	err := conn.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		fmt.Printf("[ERROR] sendPayload: %v\n", err)
	}
}

func GetAllFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func checkProgress(meta *ChunkMetadata) {
	pattern := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.*", meta.FileName))
	matches, _ := filepath.Glob(pattern)
	count := len(matches)
	percent := (float64(count) / float64(meta.NumChunks)) * 100

	fmt.Printf("\r[RECEIVER] progress: %.2f%% (%d/%d chunks)", percent, count, meta.NumChunks)

	if count == meta.NumChunks {
		fmt.Println("\n[RECEIVER] Reassembling...")
		if err := ReassembleFile(*meta); err == nil {
			VerifyAndFinalize(*meta)
			for _, m := range matches {
				os.Remove(m)
			}
		}
	}
}

func saveChunk(fileName string, index uint32, data []byte) {
	path := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", fileName, index))
	os.WriteFile(path, data, 0644)
}

func wrapChunk(index int, data []byte) []byte {
	buf := make([]byte, 5+len(data))
	buf[0] = 0xAA
	binary.BigEndian.PutUint32(buf[1:5], uint32(index))
	copy(buf[5:], data)
	return buf
}

func ReassembleFile(meta ChunkMetadata) error {
	finalPath := filepath.Join(dir, meta.FileName)
	tempPath := finalPath + ".tmp"

	if _, err := os.Stat(finalPath); err == nil {
		isMatch, _ := VerifyExistingFile(meta, finalPath)
		if isMatch {
			return cleanupChunks(meta)
		}
	}

	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}

	for i := 0; i < meta.NumChunks; i++ {
		chunkPath := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", meta.FileName, i))
		content, _ := os.ReadFile(chunkPath)
		out.Write(content)
	}
	out.Close()
	os.Rename(tempPath, finalPath)
	os.Chtimes(finalPath, time.Now(), meta.ModTime)
	return cleanupChunks(meta)
}

func VerifyAndFinalize(meta ChunkMetadata) {
	finalPath := filepath.Join(dir, meta.FileName)
	f, _ := os.Open(finalPath)
	defer f.Close()

	for i, expectedHash := range meta.ChunkHashes {
		buf := make([]byte, meta.ChunkSize)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			break
		}

		actualHash := fmt.Sprintf("%x", sha256.Sum256(buf[:n]))

		if actualHash != expectedHash {
			fmt.Printf("[ERROR] chunk %d mismatch: expected %s, got %s\n", i, expectedHash, actualHash)
			return
		}
	}
	fmt.Println("[SYSTEM] File verified bit-perfect.")
}

func VerifyExistingFile(meta ChunkMetadata, filePath string) (bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	for _, expectedHash := range meta.ChunkHashes {
		buf := make([]byte, meta.ChunkSize)
		n, _ := f.Read(buf)
		actualHash := fmt.Sprintf("%x", sha256.Sum256(buf[:n]))
		if actualHash != expectedHash {
			return false, nil
		}
	}
	return true, nil
}

func cleanupChunks(meta ChunkMetadata) error {
	for i := 0; i < meta.NumChunks; i++ {
		os.Remove(filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", meta.FileName, i)))
	}
	return nil
}
