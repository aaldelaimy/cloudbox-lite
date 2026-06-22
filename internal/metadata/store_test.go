package metadata

import (
	"testing"
)

// newTestStore creates an in-memory SQLite database for testing.
// Nothing is written to disk.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store
}

func sampleUploadRequest() UploadRequest {
	return UploadRequest{
		Filename:  "resume.pdf",
		SizeBytes: 86000,
		FileHash:  "abc123",
		Chunks: []ChunkInfo{
			{Index: 0, Hash: "h1", SizeBytes: 32768, Nodes: []string{"node-1"}},
			{Index: 1, Hash: "h2", SizeBytes: 32768, Nodes: []string{"node-1"}},
			{Index: 2, Hash: "h3", SizeBytes: 20464, Nodes: []string{"node-1"}},
		},
	}
}

func TestSaveAndGetFile(t *testing.T) {
	store := newTestStore(t)

	req := sampleUploadRequest()
	resp, err := store.SaveFile(req)
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}
	if resp.Version != 1 {
		t.Errorf("expected version 1, got %d", resp.Version)
	}

	got, err := store.GetLatestFile("resume.pdf")
	if err != nil {
		t.Fatalf("GetLatestFile failed: %v", err)
	}
	if got.Filename != "resume.pdf" {
		t.Errorf("expected filename resume.pdf, got %q", got.Filename)
	}
	if got.FileHash != "abc123" {
		t.Errorf("expected file hash abc123, got %q", got.FileHash)
	}
	if len(got.Chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(got.Chunks))
	}
}

func TestChunkOrder(t *testing.T) {
	store := newTestStore(t)

	store.SaveFile(sampleUploadRequest())

	got, err := store.GetLatestFile("resume.pdf")
	if err != nil {
		t.Fatalf("GetLatestFile failed: %v", err)
	}

	for i, chunk := range got.Chunks {
		if chunk.Index != i {
			t.Errorf("expected chunk index %d, got %d", i, chunk.Index)
		}
	}
}

func TestSameContentNoNewVersion(t *testing.T) {
	store := newTestStore(t)

	req := sampleUploadRequest()
	store.SaveFile(req)

	resp, err := store.SaveFile(req)
	if err != nil {
		t.Fatalf("second SaveFile failed: %v", err)
	}
	if resp.Version != 1 {
		t.Errorf("expected version to stay 1 for same content, got %d", resp.Version)
	}
}

func TestChangedContentNewVersion(t *testing.T) {
	store := newTestStore(t)

	req := sampleUploadRequest()
	store.SaveFile(req)

	req.FileHash = "differenthash"
	req.Chunks = []ChunkInfo{
		{Index: 0, Hash: "h4", SizeBytes: 32768, Nodes: []string{"node-1"}},
	}

	resp, err := store.SaveFile(req)
	if err != nil {
		t.Fatalf("second SaveFile failed: %v", err)
	}
	if resp.Version != 2 {
		t.Errorf("expected version 2 for changed content, got %d", resp.Version)
	}
}

func TestStoreListFiles(t *testing.T) {
	store := newTestStore(t)

	store.SaveFile(sampleUploadRequest())

	req2 := sampleUploadRequest()
	req2.Filename = "notes.txt"
	req2.FileHash = "noteshash"
	store.SaveFile(req2)

	files, err := store.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestStoreGetFileNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetLatestFile("nonexistent.pdf")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}