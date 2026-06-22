package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"cloudbox-lite/internal/hashring"
	"cloudbox-lite/internal/hashing"
	"cloudbox-lite/internal/metadata"
)

// NodeConfig holds the ID and address of one block storage node.
type NodeConfig struct {
	ID      string
	Address string
}

// Config holds the addresses of the metadata server and block servers.
type Config struct {
	MetadataAddr      string
	BlockAddr         string
	BlockNodes        []NodeConfig
	ReplicationFactor int
}

// Client handles communication with the metadata and block servers.
type Client struct {
	config Config
	http   *http.Client
}

// NewClient creates a new Client with the given config.
func NewClient(config Config) *Client {
	if config.ReplicationFactor <= 0 {
		config.ReplicationFactor = 1
	}
	return &Client{
		config: config,
		http:   &http.Client{},
	}
}

// GetConfig returns the client's config.
func (c *Client) GetConfig() Config {
	return c.config
}

// buildRing constructs a hash ring from the configured block nodes.
func (c *Client) buildRing() *hashring.Ring {
	ring := hashring.NewRing(100)
	for _, node := range c.config.BlockNodes {
		ring.AddNode(node.ID, node.Address)
	}
	return ring
}

// chunkExists checks if a chunk already exists on a node using HEAD.
func (c *Client) chunkExists(address string, hash string) bool {
	url := fmt.Sprintf("http://%s/chunks/%s", address, hash)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// uploadChunkToNode sends a chunk to a specific node address.
// Checks if the chunk already exists before uploading.
func (c *Client) uploadChunkToNode(address string, hash string, data []byte) (bool, error) {
	if c.chunkExists(address, hash) {
		return true, nil
	}

	url := fmt.Sprintf("http://%s/chunks/%s", address, hash)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to upload chunk: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return false, fmt.Errorf("block server returned %d", resp.StatusCode)
	}

	var result struct {
		Duplicate bool `json:"duplicate"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	return result.Duplicate, nil
}

// UploadChunk sends a chunk to the default block server.
func (c *Client) UploadChunk(hash string, data []byte) (bool, error) {
	return c.uploadChunkToNode(c.config.BlockAddr, hash, data)
}

// RegisterFile sends file metadata to the metadata server.
func (c *Client) RegisterFile(req metadata.UploadRequest) (*metadata.UploadResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode metadata: %w", err)
	}

	url := fmt.Sprintf("http://%s/files", c.config.MetadataAddr)
	resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to register file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata server returned %d", resp.StatusCode)
	}

	var result metadata.UploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetFileMetadata retrieves file metadata from the metadata server.
func (c *Client) GetFileMetadata(filename string) (*metadata.FileMetadata, error) {
	url := fmt.Sprintf("http://%s/files/%s", c.config.MetadataAddr, filename)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file %q not found", filename)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata server returned %d", resp.StatusCode)
	}

	var result metadata.FileMetadata
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	return &result, nil
}

// DownloadChunkFromNode fetches a chunk from a specific node address.
func (c *Client) DownloadChunkFromNode(address string, hash string) ([]byte, error) {
	url := fmt.Sprintf("http://%s/chunks/%s", address, hash)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download chunk %s: %w", hash, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("chunk %s not found on %s", hash, address)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("block server returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk: %w", err)
	}

	return data, nil
}

// DownloadChunk fetches a chunk from the default block server.
func (c *Client) DownloadChunk(hash string) ([]byte, error) {
	return c.DownloadChunkFromNode(c.config.BlockAddr, hash)
}

// ListFiles returns a summary of all files from the metadata server.
func (c *Client) ListFiles() ([]metadata.FileListItem, error) {
	url := fmt.Sprintf("http://%s/files", c.config.MetadataAddr)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata server returned %d", resp.StatusCode)
	}

	var files []metadata.FileListItem
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return files, nil
}

// InspectFile returns full metadata for one file including all chunk details.
func (c *Client) InspectFile(filename string) (*metadata.FileMetadata, error) {
	url := fmt.Sprintf("http://%s/files/%s/inspect", c.config.MetadataAddr, filename)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file %q not found", filename)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata server returned %d", resp.StatusCode)
	}

	var file metadata.FileMetadata
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &file, nil
}

// CheckHealth checks whether a server is alive by hitting its /health endpoint.
func (c *Client) CheckHealth(address string) bool {
	url := fmt.Sprintf("http://%s/health", address)
	resp, err := c.http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// UploadSummary holds the result of an upload operation.
type UploadSummary struct {
	Filename        string
	Version         int
	SizeBytes       int64
	ChunkCount      int
	NewChunks       int
	DuplicateChunks int
	FileHash        string
}

// Upload performs the full upload flow for a file.
func (c *Client) Upload(path string, chunkSize int) (*UploadSummary, error) {
	fileHash, err := hashing.HashFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file: %w", err)
	}

	chunks, err := chunkFile(path, chunkSize)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk file: %w", err)
	}

	if len(c.config.BlockNodes) == 0 {
		return nil, fmt.Errorf("no block nodes configured")
	}

	ring := c.buildRing()

	var chunkInfos []metadata.ChunkInfo
	newChunks := 0
	duplicateChunks := 0
	var totalSize int64

	for _, chunk := range chunks {
		totalSize += int64(chunk.size)

		nodes, err := ring.GetNodes(chunk.hash, c.config.ReplicationFactor)
		if err != nil {
			return nil, fmt.Errorf("failed to get nodes for chunk %d: %w", chunk.index, err)
		}

		var nodeIDs []string
		duplicate := true
		for _, node := range nodes {
			dup, err := c.uploadChunkToNode(node.Address, chunk.hash, chunk.data)
			if err != nil {
				fmt.Printf("warning: failed to upload chunk %s to %s: %v\n", chunk.hash, node.ID, err)
				continue
			}
			if !dup {
				duplicate = false
			}
			nodeIDs = append(nodeIDs, node.ID)
		}

		if len(nodeIDs) == 0 {
			return nil, fmt.Errorf("failed to upload chunk %d to any node", chunk.index)
		}

		if duplicate {
			duplicateChunks++
		} else {
			newChunks++
		}

		chunkInfos = append(chunkInfos, metadata.ChunkInfo{
			Index:     chunk.index,
			Hash:      chunk.hash,
			SizeBytes: chunk.size,
			Nodes:     nodeIDs,
		})
	}

	req := metadata.UploadRequest{
		Filename:  pathToFilename(path),
		SizeBytes: totalSize,
		FileHash:  fileHash,
		Chunks:    chunkInfos,
	}

	uploadResp, err := c.RegisterFile(req)
	if err != nil {
		return nil, fmt.Errorf("failed to register metadata: %w", err)
	}

	return &UploadSummary{
		Filename:        uploadResp.Filename,
		Version:         uploadResp.Version,
		SizeBytes:       totalSize,
		ChunkCount:      len(chunks),
		NewChunks:       newChunks,
		DuplicateChunks: duplicateChunks,
		FileHash:        fileHash,
	}, nil
}