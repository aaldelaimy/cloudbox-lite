package client

import (
	"fmt"
	"path/filepath"
	"strings"

	"cloudbox-lite/internal/chunker"
	"cloudbox-lite/internal/hashing"
)

// chunk is an internal representation of a file chunk with its hash attached.
type chunk struct {
	index int
	data  []byte
	hash  string
	size  int
}

// chunkFile opens the file at path, splits it into chunks, and hashes each one.
func chunkFile(path string, chunkSize int) ([]chunk, error) {
	rawChunks, err := chunker.ChunkFile(path, chunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk file: %w", err)
	}

	var chunks []chunk
	for _, c := range rawChunks {
		chunks = append(chunks, chunk{
			index: c.Index,
			data:  c.Data,
			hash:  hashing.HashBytes(c.Data),
			size:  c.Size,
		})
	}

	return chunks, nil
}

// pathToFilename extracts just the filename from a full path.
// "./docs/resume.pdf" -> "resume.pdf"
func pathToFilename(path string) string {
	base := filepath.Base(path)
	return strings.TrimPrefix(base, ".")
}