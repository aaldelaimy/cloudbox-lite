package chunker

import (
	"fmt"
	"io"
	"os"
)

const DefaultChunkSize = 32 * 1024 // 32 KB

// Chunk represents one piece of a file.
type Chunk struct {
	Index int    // position of this chunk in the original file (0-based)
	Data  []byte // the raw bytes of this chunk
	Size  int    // how many bytes are in Data
}

// ChunkFile opens the file at path and splits it into chunks of chunkSize bytes.
// Returns an ordered slice of Chunks, or an error if the file cannot be read.
// An empty file returns an empty slice and no error.
func ChunkFile(path string, chunkSize int) ([]Chunk, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunkSize must be greater than zero, got %d", chunkSize)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer file.Close()

	return chunkReader(file, chunkSize)
}

// chunkReader does the actual work and accepts any io.Reader.
// It is separate from ChunkFile so tests can pass bytes directly
// without needing real files on disk.
func chunkReader(r io.Reader, chunkSize int) ([]Chunk, error) {
	var chunks []Chunk
	index := 0

	for {
		buf := make([]byte, chunkSize)
		n, err := io.ReadFull(r, buf)

		if n > 0 {
			chunks = append(chunks, Chunk{
				Index: index,
				Data:  buf[:n],
				Size:  n,
			})
			index++
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("error reading chunk %d: %w", index, err)
		}
	}

	return chunks, nil
}