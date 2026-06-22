package hashing

import (
	"bytes"
	"testing"
)

func TestHashBytesIsDeterministic(t *testing.T) {
	// same input must always produce the same hash
	data := []byte("hello cloudbox")
	hash1 := HashBytes(data)
	hash2 := HashBytes(data)

	if hash1 != hash2 {
		t.Errorf("expected same hash for same input, got %q and %q", hash1, hash2)
	}
}

func TestHashBytesDifferentInputs(t *testing.T) {
	// different inputs must produce different hashes
	hash1 := HashBytes([]byte("hello"))
	hash2 := HashBytes([]byte("hellp"))

	if hash1 == hash2 {
		t.Errorf("expected different hashes for different inputs, got the same: %q", hash1)
	}
}

func TestHashBytesEmptyInput(t *testing.T) {
	// empty bytes should still produce a valid hash, not crash or return empty string
	hash := HashBytes([]byte{})

	if hash == "" {
		t.Error("expected non-empty hash for empty input")
	}
	if len(hash) != 64 {
		t.Errorf("expected 64 character hex string, got length %d", len(hash))
	}
}

func TestHashBytesIsHexString(t *testing.T) {
	// output should always be exactly 64 hex characters
	hash := HashBytes([]byte("any content here"))

	if len(hash) != 64 {
		t.Errorf("expected 64 character hex string, got length %d: %q", len(hash), hash)
	}
}

func TestHashReaderMatchesHashBytes(t *testing.T) {
	// hashing a reader with the same bytes should produce the same result as HashBytes
	data := []byte("the quick brown fox")

	fromBytes := HashBytes(data)
	fromReader, err := hashReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fromBytes != fromReader {
		t.Errorf("HashBytes and hashReader produced different results for same data\nHashBytes:   %q\nhashReader:  %q", fromBytes, fromReader)
	}
}

func TestHashReaderEmptyInput(t *testing.T) {
	hash, err := hashReader(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("expected 64 character hex string, got length %d", len(hash))
	}
}