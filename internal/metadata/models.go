package metadata

import "time"

// FileMetadata represents everything the metadata server knows about a file.
// It does not contain the actual file bytes - just the information needed
// to locate and reconstruct the file from block storage nodes.
type FileMetadata struct {
	ID        int64      // unique database ID
	Filename  string     // original filename, e.g. "resume.pdf"
	Version   int        // version number, starts at 1, increments on change
	SizeBytes int64      // total file size in bytes
	FileHash  string     // SHA-256 hash of the full file, used to verify downloads
	Chunks    []ChunkRef // ordered list of chunks needed to reconstruct the file
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChunkRef describes one chunk within a file.
// The Index field is critical - chunks must be reassembled in index order.
type ChunkRef struct {
	Index     int      // position of this chunk in the file (0-based)
	Hash      string   // SHA-256 hash of this chunk's bytes, used as its ID
	SizeBytes int      // size of this chunk in bytes
	Nodes     []string // IDs of the block storage nodes that have this chunk
}

// StorageNode represents one block storage node in the system.
type StorageNode struct {
	ID      string // unique identifier, e.g. "node-1"
	Address string // network address, e.g. "localhost:9001"
	Healthy bool   // whether this node responded to its last health check
}

// UploadRequest is the JSON body the CLI sends to POST /files.
type UploadRequest struct {
	Filename  string      `json:"filename"`
	SizeBytes int64       `json:"size_bytes"`
	FileHash  string      `json:"file_hash"`
	Chunks    []ChunkInfo `json:"chunks"`
}

// ChunkInfo is the per-chunk data inside an UploadRequest.
type ChunkInfo struct {
	Index     int      `json:"index"`
	Hash      string   `json:"hash"`
	SizeBytes int      `json:"size_bytes"`
	Nodes     []string `json:"nodes"`
}

// UploadResponse is what the metadata server sends back after POST /files.
type UploadResponse struct {
	Filename string `json:"filename"`
	Version  int    `json:"version"`
}

// FileListItem is one entry in the response to GET /files.
type FileListItem struct {
	Filename  string `json:"filename"`
	Version   int    `json:"version"`
	SizeBytes int64  `json:"size_bytes"`
	ChunkCount int   `json:"chunk_count"`
}