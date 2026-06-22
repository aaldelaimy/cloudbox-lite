package blockstore

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"cloudbox-lite/internal/hashing"
)

// Handler holds a reference to the Store and exposes HTTP endpoints.
type Handler struct {
	store *Store
}

// NewHandler creates a Handler backed by the given Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes attaches the handler methods to the given ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/chunks/", h.handleChunk)
	mux.HandleFunc("/health", h.handleHealth)
}

// handleChunk routes PUT, GET, and HEAD requests for /chunks/{hash}.
func (h *Handler) handleChunk(w http.ResponseWriter, r *http.Request) {
	// extract the hash from the URL path
	// URL looks like /chunks/a82f9c...
	hash := strings.TrimPrefix(r.URL.Path, "/chunks/")
	if hash == "" {
		http.Error(w, "missing chunk hash in URL", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.putChunk(w, r, hash)
	case http.MethodGet:
		h.getChunk(w, r, hash)
	case http.MethodHead:
		h.headChunk(w, r, hash)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// putChunk stores a chunk. Verifies the body hash matches the URL hash.
func (h *Handler) putChunk(w http.ResponseWriter, r *http.Request, hash string) {
	// read the entire request body into memory
	data, err := readBody(r, 10*1024*1024) // 10 MB limit
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}

	// verify the body actually matches the hash in the URL
	// the server never blindly trusts the client
	actual := hashing.HashBytes(data)
	if actual != hash {
		http.Error(w, "hash mismatch: body does not match URL hash", http.StatusBadRequest)
		return
	}

	duplicate, err := h.store.Put(hash, data)
	if err != nil {
		http.Error(w, "failed to store chunk", http.StatusInternalServerError)
		return
	}

	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]bool{
		"stored":    true,
		"duplicate": duplicate,
	})
}

// getChunk retrieves a chunk's raw bytes by hash.
func (h *Handler) getChunk(w http.ResponseWriter, r *http.Request, hash string) {
	data, err := h.store.Get(hash)
	if err != nil {
		if errors.Is(err, ErrChunkNotFound) {
			http.Error(w, "chunk not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to retrieve chunk", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// headChunk checks whether a chunk exists without returning its bytes.
func (h *Handler) headChunk(w http.ResponseWriter, r *http.Request, hash string) {
	if h.store.Has(hash) {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// handleHealth responds to health check requests.
func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// readBody reads the entire request body up to maxBytes.
func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}