package relayclient

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"os/user"
	"path/filepath"
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

var activeDownloadMeta *ChunkMetadata
var filesMetadata []ChunkMetadata
var dir string

func LaunchRelayClient() {
	// create necessary directories
	os.MkdirAll("temp_chunks", os.ModePerm)
	os.MkdirAll("chunks", os.ModePerm)
	// url := "https://dashboard.heroku.com/apps/relaysvr-didactic-lemon-fiesta"
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
	dir := "/Users/" + currentUser.Username + "/Downloads"

	filesMetadata, err := scanForFiles(dir)
	if err != nil {
		log.Fatalf("Failed to scan for files: %v", err)
	}

	host := "relaysvr-didactic-lemon-fiesta.herokuapp.com:443"

	tlsConfig := &tls.Config{
		ServerName: "relaysvr-didactic-lemon-fiesta.herokuapp.com",
	}

	conn, err := tls.Dial("tcp", host, tlsConfig)
	if err != nil {
		fmt.Printf("[ERROR] TLS connection failed: %v\n", err)
		return
	}

	fmt.Println("[SYSTEM] Connected securely to Heroku Relay!")
	defer conn.Close()

	// register with the relay server
	fmt.Printf("[SYSTEM] Registering as %s...\n", clientID)
	// conn.Write([]byte{byte(len(clientID))})
	// conn.Write([]byte(clientID))
	// conn.Write([]byte{byte(len(secret))})
	// conn.Write([]byte(secret))
	regBuf := []byte{}
	regBuf = append(regBuf, byte(len(clientID)))
	regBuf = append(regBuf, []byte(clientID)...)
	regBuf = append(regBuf, byte(len(secret)))
	regBuf = append(regBuf, []byte(secret)...)

	n, err := conn.Write(regBuf)
	if err != nil {
		fmt.Printf("[ERROR] Registration write failed: %v\n", err)
		return
	}
	fmt.Printf("[DEBUG] Wrote %d bytes to server\n", n)

	connectedSignal := make(chan string, 1)

	// main listener
	go func() {
		fmt.Println("[DEBUG] Listener goroutine started")
		defer close(connectedSignal)
		var peerID string

		for {
			conn.SetReadDeadline(time.Now().Add(300 * time.Second))

			// read size
			var pSize uint32
			err := binary.Read(conn, binary.BigEndian, &pSize)
			if err != nil {
				return
			}

			if pSize == 0 {
				// it's a keep-alive ping
				continue
			}

			fmt.Printf("[LOG] new packet detected. Size: %d bytes\n", pSize)

			payload := make([]byte, pSize)
			n, err := io.ReadFull(conn, payload)
			if err != nil {
				fmt.Printf("[LOG] error reading payload: expected %d bytes, only got %d. error: %v\n", pSize, n, err)
				return
			}
			dataStr := string(payload)

			if len(payload) == 0 {
				continue
			}

			headerByte := payload[0]

			switch headerByte {
			case 0xAA:
				if len(payload) < 5 {
					continue
				}
				index := binary.BigEndian.Uint32(payload[1:5])
				fmt.Printf("[LOG] extracted chunk index %d\n", index)

				if activeDownloadMeta != nil {
					fmt.Printf("[LOG] writing chunk %d for file %s\n", index, activeDownloadMeta.FileName)
					saveChunk(activeDownloadMeta.FileName, index, payload[5:])
					checkProgress(activeDownloadMeta)
				} else {
					fmt.Println("[LOG] error: activeDownloadMeta is nil, cannot save chunk")
				}
				continue

			case '{':
				var generic map[string]interface{}
				if err := json.Unmarshal(payload, &generic); err != nil {
					fmt.Printf("[LOG] unmarshal failed: %v\n", err)
					continue
				}
				fmt.Printf("[LOG] parsed keys: %v\n", getMapKeys(generic))

				if _, ok := generic["file_name"]; ok {
					var incomingMeta ChunkMetadata
					if err := json.Unmarshal(payload, &incomingMeta); err == nil {
						fmt.Printf("[RECEIVER] manifest: %s (%d chunks)\n", incomingMeta.FileName, incomingMeta.NumChunks)
						activeDownloadMeta = &incomingMeta
						go handleIncomingMetadata(conn, incomingMeta)
					} else {
						fmt.Printf("[LOG] failed to map into ChunkMetadata struct: %v\n", err)
					}
					continue
				}

				if val, ok := generic["type"].(string); ok && val == "CHUNK_REQ" {
					file, okF := generic["file"].(string)
					indexFloat, okI := generic["index"].(float64) // JSON numbers are float64 in Go maps

					if !okF || !okI {
						continue
					}

					index := int(indexFloat)
					fmt.Printf("[SENDER] processing request: %s (Part %d)\n", file, index)

					chunkPath := filepath.Join("chunks", fmt.Sprintf("%s.chunk.%d", file, index))

					chunkData, err := os.ReadFile(chunkPath)
					if err != nil {
						fmt.Printf("[ERROR] sender disk read failed for chunk %d: %v\n", index, err)
						continue
					}

					fmt.Printf("[LOG] sender sending chunk %d (%d bytes)\n", index, len(chunkData))
					sendPayload(conn, wrapChunk(index, chunkData))
					fmt.Printf("[SENDER] successfully sent chunk %d\n", index)
					continue
				}

			case 'C':
				if strings.HasPrefix(dataStr, "CONNECTED:") {
					peerID = strings.TrimPrefix(dataStr, "CONNECTED:")
					connectedSignal <- peerID
					continue
				}

			default:
				fmt.Printf("[LOG] unhandled payload: %s\n", dataStr)
				continue
			}
		}
	}()

	fmt.Println("[SYSTEM] Awaiting client connection...")
	_, ok := <-connectedSignal
	if !ok {
		return
	}

	for _, metadata := range filesMetadata {
		time.Sleep(500 * time.Millisecond)
		metaJSON, _ := json.Marshal(metadata)
		sendPayload(conn, metaJSON)
		fmt.Printf("[SENDER] Metadata pushed (Size: %d)\n", len(metaJSON))
	}

	for {
		binary.Write(conn, binary.BigEndian, uint32(0))
		time.Sleep(20 * time.Second)
	}
}

func scanForFiles(dir string) ([]ChunkMetadata, error) {
	// scan Downloads directory for files

	files, err := GetAllFiles(dir)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d files in %s and its subdirectories:\n", len(files), dir)
	var metadata *ChunkMetadata
	var allMetadata []ChunkMetadata
	for _, file := range files {
		fmt.Println(file)
		info, err := os.Stat(file)
		if err != nil {
			log.Printf("Could not stat file %s: %v", file, err)
			continue
		}

		// Get the last modified time
		modTime := info.ModTime()

		if file != "" {
			var err error
			metadata, err = splitFile(file, modTime)
			if err != nil {
				fmt.Printf("[ERROR] file split failed: %v\n", err)
				return nil, err
			}
			allMetadata = append(allMetadata, *metadata)
		}
	}

	return allMetadata, nil
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// monitors download completion
func checkProgress(meta *ChunkMetadata) {
	pattern := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.*", meta.FileName))
	matches, _ := filepath.Glob(pattern)
	count := len(matches)
	percent := (float64(count) / float64(meta.NumChunks)) * 100

	fmt.Printf("\r[RECEIVER] download progress: %.2f%% (%d/%d chunks)", percent, count, meta.NumChunks)

	if count == meta.NumChunks {
		fmt.Println("\n[RECEIVER] Transfer complete, reassembling...")
		if err := ReassembleFile(*meta); err == nil {
			VerifyAndFinalize(*meta)
			for _, m := range matches {
				os.Remove(m)
			}
		}
	}
}

// requests all chunks from the peer
func handleIncomingMetadata(conn net.Conn, meta ChunkMetadata) {

	// add check for existing file and comparision

	// Safety delay
	time.Sleep(300 * time.Millisecond)

	for i, hash := range meta.ChunkHashes {
		fmt.Printf("[LOG] requesting chunk index: %d\n", i)

		request := map[string]interface{}{
			"type":  "CHUNK_REQ",
			"index": i,
			"hash":  hash,
			"file":  meta.FileName,
		}

		reqBytes, err := json.Marshal(request)
		if err != nil {
			continue
		}

		sendPayload(conn, reqBytes)

		fmt.Printf("[RECEIVER] request for chunk %d sent to relay\n", i)
		time.Sleep(20 * time.Millisecond)
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

func sendPayload(conn net.Conn, data []byte) {
	binary.Write(conn, binary.BigEndian, uint32(len(data)))
	conn.Write(data)
}

// merges chunks into final file
func ReassembleFile(meta ChunkMetadata) error {
	finalPath := filepath.Join(dir, meta.FileName)
	tempPath := finalPath + ".tmp"

	// check if file already exists
	if _, err := os.Stat(finalPath); err == nil {
		// file exists, verify if it's identical
		isMatch, err := VerifyExistingFile(meta, finalPath)
		if err != nil {
			return err
		}

		if isMatch {
			fmt.Printf("[SKIP] %s is already bit-perfect. Deleting incoming chunks.\n", meta.FileName)
			return cleanupChunks(meta)
		}

		// not a match, check ModTime
		existingInfo, _ := os.Stat(finalPath)
		if !meta.ModTime.After(existingInfo.ModTime()) {
			fmt.Printf("[SKIP] Existing %s is newer or same age. Dropping update.\n", meta.FileName)
			return cleanupChunks(meta)
		}
		fmt.Printf("[UPDATE] Incoming %s is newer. Replacing old file...\n", meta.FileName)
	}

	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}

	for i := 0; i < meta.NumChunks; i++ {
		chunkPath := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", meta.FileName, i))
		content, err := os.ReadFile(chunkPath)
		if err != nil {
			out.Close()
			return err
		}
		out.Write(content)
	}
	out.Close()

	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}

	// update the local file's ModTime to match the source metadata
	os.Chtimes(finalPath, time.Now(), meta.ModTime)

	fmt.Printf("Successfully finalized: %s\n", finalPath)
	return cleanupChunks(meta)
}

// post-reassembly SHA256 integrity check
func VerifyAndFinalize(meta ChunkMetadata) {
	fmt.Println("[SYSTEM] SHA256 integrity check...")
	finalPath := filepath.Join(dir, meta.FileName)
	f, _ := os.Open(finalPath)
	defer f.Close()

	for i, expectedHash := range meta.ChunkHashes {
		buf := make([]byte, meta.ChunkSize)
		n, _ := f.Read(buf)
		actualHash := fmt.Sprintf("%x", sha256.Sum256(buf[:n]))
		if actualHash != expectedHash {
			fmt.Printf("[ERROR] chunk %d mismatch: expected %s, got %s\n", i, expectedHash, actualHash)
			return
		}
	}
	fmt.Println("[SYSTEM] file is bit-perfect match")
}

func GetAllFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("Error accessing path %s: %v\n", path, err)
			return err
		}

		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking the path %s: %w", root, err)
	}

	return files, nil
}

// Returns true if the existing file matches the metadata hashes
func VerifyExistingFile(meta ChunkMetadata, filePath string) (bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	for _, expectedHash := range meta.ChunkHashes {
		buf := make([]byte, meta.ChunkSize)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return false, err
		}

		actualHash := fmt.Sprintf("%x", sha256.Sum256(buf[:n]))
		if actualHash != expectedHash {
			return false, nil // Mismatch found
		}
	}
	return true, nil // Bit-perfect match
}

func cleanupChunks(meta ChunkMetadata) error {
	for i := 0; i < meta.NumChunks; i++ {
		chunkPath := filepath.Join("temp_chunks", fmt.Sprintf("%s.part.%d", meta.FileName, i))
		os.Remove(chunkPath)
	}
	return nil
}
