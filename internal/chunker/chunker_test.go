package chunker

import (
	"bytes"
	"testing"
)

// helper so tests don't need real files
func chunkBytes(data []byte, chunkSize int) ([]Chunk, error) {
	return chunkReader(bytes.NewReader(data), chunkSize)
}

func TestEmptyFile(t *testing.T) {
	chunks, err := chunkBytes([]byte{}, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks, got %d", len(chunks))
	}
}

func TestFileSmallerThanChunkSize(t *testing.T) {
	data := []byte("hello")
	chunks, err := chunkBytes(data, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Index != 0 {
		t.Errorf("expected index 0, got %d", chunks[0].Index)
	}
	if chunks[0].Size != len(data) {
		t.Errorf("expected size %d, got %d", len(data), chunks[0].Size)
	}
	if !bytes.Equal(chunks[0].Data, data) {
		t.Errorf("chunk data does not match original")
	}
}

func TestFileExactlyChunkSize(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 1024)
	chunks, err := chunkBytes(data, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Size != 1024 {
		t.Errorf("expected size 1024, got %d", chunks[0].Size)
	}
}

func TestFileSlightlyLargerThanChunkSize(t *testing.T) {
	// 1025 bytes, chunk size 1024 → chunk 0 is 1024 bytes, chunk 1 is 1 byte
	data := bytes.Repeat([]byte("B"), 1025)
	chunks, err := chunkBytes(data, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Size != 1024 {
		t.Errorf("expected first chunk size 1024, got %d", chunks[0].Size)
	}
	if chunks[1].Size != 1 {
		t.Errorf("expected second chunk size 1, got %d", chunks[1].Size)
	}
}

func TestChunkOrder(t *testing.T) {
	// three distinct 4-byte sections so we can verify order
	part0 := bytes.Repeat([]byte{0x00}, 4)
	part1 := bytes.Repeat([]byte{0x11}, 4)
	part2 := bytes.Repeat([]byte{0x22}, 4)
	data := append(append(part0, part1...), part2...)

	chunks, err := chunkBytes(data, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	expected := [][]byte{part0, part1, part2}
	for i, exp := range expected {
		if chunks[i].Index != i {
			t.Errorf("chunk %d has wrong index %d", i, chunks[i].Index)
		}
		if !bytes.Equal(chunks[i].Data, exp) {
			t.Errorf("chunk %d data mismatch", i)
		}
	}
}

func TestReconstruction(t *testing.T) {
	// the most important guarantee: concat all chunks in order → original file
	original := []byte("the quick brown fox jumps over the lazy dog")
	chunks, err := chunkBytes(original, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var reconstructed []byte
	for _, c := range chunks {
		reconstructed = append(reconstructed, c.Data...)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("reconstruction failed\nwant: %q\ngot:  %q", original, reconstructed)
	}
}

func TestInvalidChunkSize(t *testing.T) {
	_, err := ChunkFile("anything", 0)
	if err == nil {
		t.Error("expected error for chunkSize=0, got nil")
	}
}