package metadata

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Server wraps the Store and exposes it over HTTP.
type Server struct {
	store *Store
}

// NewServer creates a new Server backed by the given Store.
func NewServer(store *Store) *Server {
	return &Server{store: store}
}

// RegisterRoutes attaches all handlers to the given ServeMux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/files", s.handleFiles)
	mux.HandleFunc("/files/", s.handleFileByName)
	mux.HandleFunc("/health", s.handleHealth)
}

// handleFiles routes POST /files and GET /files.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.uploadFile(w, r)
	case http.MethodGet:
		s.listFiles(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFileByName routes GET /files/{filename} and GET /files/{filename}/inspect.
func (s *Server) handleFileByName(w http.ResponseWriter, r *http.Request) {
	// strip the /files/ prefix to get the rest of the path
	rest := strings.TrimPrefix(r.URL.Path, "/files/")
	if rest == "" {
		http.Error(w, "missing filename", http.StatusBadRequest)
		return
	}

	// check if this is an inspect request: /files/{filename}/inspect
	if strings.HasSuffix(rest, "/inspect") {
		filename := strings.TrimSuffix(rest, "/inspect")
		s.inspectFile(w, r, filename)
		return
	}

	// otherwise it's a plain GET /files/{filename}
	s.getFile(w, r, rest)
}

// uploadFile handles POST /files.
// The CLI sends file metadata here after uploading chunks to block servers.
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	var req UploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Filename == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
		return
	}
	if req.FileHash == "" {
		http.Error(w, "file_hash is required", http.StatusBadRequest)
		return
	}
	if len(req.Chunks) == 0 {
		http.Error(w, "chunks are required", http.StatusBadRequest)
		return
	}

	resp, err := s.store.SaveFile(req)
	if err != nil {
		http.Error(w, "failed to save file metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// getFile handles GET /files/{filename}.
// Returns the latest version of a file's metadata.
func (s *Server) getFile(w http.ResponseWriter, r *http.Request, filename string) {
	file, err := s.store.GetLatestFile(filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, file)
}

// listFiles handles GET /files.
// Returns a summary of all files at their latest versions.
func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.ListFiles()
	if err != nil {
		http.Error(w, "failed to list files", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, files)
}

// inspectFile handles GET /files/{filename}/inspect.
// Returns the full metadata including all chunk hashes and node locations.
func (s *Server) inspectFile(w http.ResponseWriter, r *http.Request, filename string) {
	file, err := s.store.GetLatestFile(filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, file)
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// writeJSON is a helper that writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// decodeError is a sentinel used to distinguish "file not found" from other errors.
var errNotFound = errors.New("not found")