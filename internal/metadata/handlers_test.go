package metadata

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer creates a Server backed by an in-memory store.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	server := NewServer(store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func TestHealthEndpoint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUploadAndGetFile(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// upload a file
	req := UploadRequest{
		Filename:  "resume.pdf",
		SizeBytes: 86000,
		FileHash:  "abc123",
		Chunks: []ChunkInfo{
			{Index: 0, Hash: "h1", SizeBytes: 32768, Nodes: []string{"node-1"}},
			{Index: 1, Hash: "h2", SizeBytes: 32768, Nodes: []string{"node-1"}},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/files", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var uploadResp UploadResponse
	json.NewDecoder(resp.Body).Decode(&uploadResp)
	if uploadResp.Version != 1 {
		t.Errorf("expected version 1, got %d", uploadResp.Version)
	}

	// get the file back
	getResp, err := http.Get(ts.URL + "/files/resume.pdf")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", getResp.StatusCode)
	}

	var fileMeta FileMetadata
	json.NewDecoder(getResp.Body).Decode(&fileMeta)
	if fileMeta.Filename != "resume.pdf" {
		t.Errorf("expected resume.pdf, got %q", fileMeta.Filename)
	}
	if len(fileMeta.Chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(fileMeta.Chunks))
	}
}

func TestGetFileNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/files/nonexistent.pdf")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListFiles(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// upload two files
	for _, filename := range []string{"a.txt", "b.txt"} {
		req := UploadRequest{
			Filename:  filename,
			SizeBytes: 100,
			FileHash:  filename + "hash",
			Chunks: []ChunkInfo{
				{Index: 0, Hash: filename + "chunk", SizeBytes: 100, Nodes: []string{"node-1"}},
			},
		}
		body, _ := json.Marshal(req)
		http.Post(ts.URL+"/files", "application/json", bytes.NewReader(body))
	}

	resp, err := http.Get(ts.URL + "/files")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var files []FileListItem
	json.NewDecoder(resp.Body).Decode(&files)
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestInspectFile(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req := UploadRequest{
		Filename:  "notes.txt",
		SizeBytes: 500,
		FileHash:  "noteshash",
		Chunks: []ChunkInfo{
			{Index: 0, Hash: "nh1", SizeBytes: 500, Nodes: []string{"node-1", "node-2"}},
		},
	}
	body, _ := json.Marshal(req)
	http.Post(ts.URL+"/files", "application/json", bytes.NewReader(body))

	resp, err := http.Get(ts.URL + "/files/notes.txt/inspect")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var fileMeta FileMetadata
	json.NewDecoder(resp.Body).Decode(&fileMeta)
	if len(fileMeta.Chunks[0].Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(fileMeta.Chunks[0].Nodes))
	}
}

func TestUploadMissingFields(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// missing file_hash
	req := UploadRequest{
		Filename:  "bad.txt",
		SizeBytes: 100,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/files", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}