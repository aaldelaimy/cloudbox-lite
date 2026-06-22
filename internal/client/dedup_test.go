package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"cloudbox-lite/internal/blockstore"
	"cloudbox-lite/internal/metadata"
)

func newTestBlockServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := blockstore.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create block store: %v", err)
	}
	handler := blockstore.NewHandler(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func newTestMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()

	var mu sync.Mutex
	files := make(map[string]metadata.UploadResponse)

	mux := http.NewServeMux()
	mux.HandleFunc("/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			defer mu.Unlock()
			var req metadata.UploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			version := 1
			if existing, ok := files[req.Filename]; ok {
				version = existing.Version + 1
			}
			resp := metadata.UploadResponse{Filename: req.Filename, Version: version}
			files[req.Filename] = resp
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(resp)
		}
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

func TestDedupSkipsExistingChunks(t *testing.T) {
	blockServer := newTestBlockServer(t)
	defer blockServer.Close()

	metaServer := newTestMetadataServer(t)
	defer metaServer.Close()

	blockAddr := blockServer.URL[7:]
	metaAddr := metaServer.URL[7:]

	config := Config{
		MetadataAddr: metaAddr,
		BlockAddr:    blockAddr,
		BlockNodes: []NodeConfig{
			{ID: "node-1", Address: blockAddr},
		},
	}
	c := NewClient(config)

	summary1, err := c.Upload("../../go.mod", 1024)
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}

	summary2, err := c.Upload("../../go.mod", 1024)
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}

	if summary1.DuplicateChunks != 0 {
		t.Errorf("expected 0 duplicates on first upload, got %d", summary1.DuplicateChunks)
	}

	if summary2.DuplicateChunks != summary1.ChunkCount {
		t.Errorf("expected %d duplicates on second upload, got %d",
			summary1.ChunkCount, summary2.DuplicateChunks)
	}

	if summary2.NewChunks != 0 {
		t.Errorf("expected 0 new chunks on second upload, got %d", summary2.NewChunks)
	}
}