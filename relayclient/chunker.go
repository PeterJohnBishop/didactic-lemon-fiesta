package relayclient

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
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
	fmt.Println("Starting chunking process...")
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	fileName := stat.Name()

	chunksDir := "chunks"
	metaDir := "metadata"
	os.MkdirAll(chunksDir, os.ModePerm)
	os.MkdirAll(metaDir, os.ModePerm)

	numChunks := int((stat.Size() + int64(ChunkSize) - 1) / int64(ChunkSize))
	hashes := make([]string, 0, numChunks)

	for i := 0; i < numChunks; i++ {
		buf := make([]byte, ChunkSize)
		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("read error at chunk %d: %w", i, err)
		}

		chunk := buf[:n]
		hash := sha256.Sum256(chunk)
		hashes = append(hashes, fmt.Sprintf("%x", hash))

		chunkPath := filepath.Join(chunksDir, fmt.Sprintf("%s.chunk.%d", fileName, i))
		if err := os.WriteFile(chunkPath, chunk, 0644); err != nil {
			return nil, fmt.Errorf("failed to write chunk %d: %w", i, err)
		}
	}

	meta := &ChunkMetadata{
		FileName:    fileName,
		ChunkSize:   ChunkSize,
		NumChunks:   numChunks,
		ChunkHashes: hashes,
	}

	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	metaFilePath := filepath.Join(metaDir, fileName+".meta.json")
	os.WriteFile(metaFilePath, metaBytes, 0644)

	fmt.Printf("split into %d chunks\n", numChunks)
	return meta, nil
}
