package hashing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// HashBytes takes a byte slice and returns its SHA-256 hash as a hex string.
func HashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// HashFile opens the file at path, reads it entirely, and returns its SHA-256
// hash as a hex string. This is used to produce the full file hash before upload
// and to verify the reconstructed file after download.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file %q: %w", path, err)
	}
	defer file.Close()

	return hashReader(file)
}

// hashReader is the internal implementation that works on any io.Reader.
// Keeping it separate from HashFile makes it testable without real files.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()

	_, err := io.Copy(h, r)
	if err != nil {
		return "", fmt.Errorf("failed to hash reader: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}