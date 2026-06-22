package blockstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrChunkNotFound is returned when a requested chunk does not exist.
var ErrChunkNotFound = errors.New("chunk not found")

// Store manages chunk storage on the local filesystem.
type Store struct {
	dir string // the directory where chunks are stored
}

// NewStore creates a new Store that saves chunks in the given directory.
// It creates the directory if it does not already exist.
func NewStore(dir string) (*Store, error) {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage directory %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// chunkPath returns the full file path for a chunk with the given hash.
func (s *Store) chunkPath(hash string) string {
	return filepath.Join(s.dir, hash)
}

// Has returns true if a chunk with the given hash exists on disk.
func (s *Store) Has(hash string) bool {
	_, err := os.Stat(s.chunkPath(hash))
	return err == nil
}

// Put stores a chunk on disk. Returns true if the chunk already existed
// (duplicate), false if it was newly stored.
func (s *Store) Put(hash string, data []byte) (duplicate bool, err error) {
	if s.Has(hash) {
		return true, nil
	}

	path := s.chunkPath(hash)
	err = os.WriteFile(path, data, 0644)
	if err != nil {
		return false, fmt.Errorf("failed to write chunk %q: %w", hash, err)
	}

	return false, nil
}

// Get retrieves a chunk's bytes by hash.
// Returns ErrChunkNotFound if the chunk does not exist.
func (s *Store) Get(hash string) ([]byte, error) {
	path := s.chunkPath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrChunkNotFound
		}
		return nil, fmt.Errorf("failed to read chunk %q: %w", hash, err)
	}
	return data, nil
}

// Put stores raw bytes from a reader rather than a byte slice.
// Used by the HTTP handler to stream the request body directly to disk.
func (s *Store) PutReader(hash string, r io.Reader) (duplicate bool, err error) {
	if s.Has(hash) {
		return true, nil
	}

	path := s.chunkPath(hash)
	file, err := os.Create(path)
	if err != nil {
		return false, fmt.Errorf("failed to create chunk file %q: %w", hash, err)
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	if err != nil {
		os.Remove(path) // clean up partial file on failure
		return false, fmt.Errorf("failed to write chunk %q: %w", hash, err)
	}

	return false, nil
}