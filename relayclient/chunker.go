package relayclient

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const basePath = "/Users/peterbishop/Development/didactic-lemon-fiesta"

const ChunkSize = 1 * 1024 * 1024 // 1MB

type ChunkMetadata struct {
	FileName    string   `json:"file_name"`
	ChunkSize   int      `json:"chunk_size"`
	NumChunks   int      `json:"num_chunks"`
	ChunkHashes []string `json:"chunk_hashes"`
}

func splitFile(filePath string) (*ChunkMetadata, error) {
	fmt.Println("chunking!")
	f, err := os.Open(filePath)
	if err != nil {
		println("Error opening file:", err.Error())
		return nil, err
	}
	defer f.Close()

	chunksDir := basePath + "/chunks"
	os.MkdirAll(chunksDir, os.ModePerm)

	stat, _ := f.Stat()
	fileSize := stat.Size()
	numChunks := int((fileSize + ChunkSize - 1) / ChunkSize)

	hashes := []string{}
	for i := 0; i < numChunks; i++ {
		buf := make([]byte, ChunkSize)
		n, _ := f.Read(buf)

		chunk := buf[:n]
		hash := sha256.Sum256(chunk)
		hashes = append(hashes, fmt.Sprintf("%x", hash[:]))

		chunkPath := filepath.Join(chunksDir, fmt.Sprintf("%s.chunk.%d", stat.Name(), i))
		os.WriteFile(chunkPath, chunk, 0644)
	}

	meta := &ChunkMetadata{
		FileName:    stat.Name(),
		ChunkSize:   ChunkSize,
		NumChunks:   numChunks,
		ChunkHashes: hashes,
	}

	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(filepath.Join("metadata", stat.Name()+".meta.json"), metaBytes, 0644)

	return meta, nil
}
