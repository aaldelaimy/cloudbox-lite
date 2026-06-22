package client

import (
	"fmt"
	"os"

	"cloudbox-lite/internal/hashing"
)

// DownloadSummary holds the result of a download operation.
type DownloadSummary struct {
	Filename    string
	OutputPath  string
	SizeBytes   int64
	ChunkCount  int
	FileHash    string
	HashMatched bool
}

// Download performs the full download flow for a file.
func (c *Client) Download(filename string, outputPath string) (*DownloadSummary, error) {
	fileMeta, err := c.GetFileMetadata(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	var totalSize int64
	for _, chunkRef := range fileMeta.Chunks {
		data, err := c.downloadChunkWithFallback(chunkRef.Hash, chunkRef.Nodes)
		if err != nil {
			os.Remove(outputPath)
			return nil, fmt.Errorf("failed to download chunk %d: %w", chunkRef.Index, err)
		}

		_, err = outFile.Write(data)
		if err != nil {
			os.Remove(outputPath)
			return nil, fmt.Errorf("failed to write chunk %d: %w", chunkRef.Index, err)
		}

		totalSize += int64(len(data))
	}

	outFile.Close()

	restoredHash, err := hashing.HashFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash restored file: %w", err)
	}

	hashMatched := restoredHash == fileMeta.FileHash
	if !hashMatched {
		os.Remove(outputPath)
		return nil, fmt.Errorf(
			"hash mismatch: restored file is corrupted\nexpected: %s\ngot:      %s",
			fileMeta.FileHash, restoredHash,
		)
	}

	return &DownloadSummary{
		Filename:    filename,
		OutputPath:  outputPath,
		SizeBytes:   totalSize,
		ChunkCount:  len(fileMeta.Chunks),
		FileHash:    restoredHash,
		HashMatched: true,
	}, nil
}

// downloadChunkWithFallback tries each node in order until one returns the chunk.
func (c *Client) downloadChunkWithFallback(hash string, nodeIDs []string) ([]byte, error) {
	var lastErr error

	for _, nodeID := range nodeIDs {
		address, err := c.nodeAddress([]string{nodeID})
		if err != nil {
			lastErr = err
			continue
		}

		data, err := c.DownloadChunkFromNode(address, hash)
		if err != nil {
			lastErr = err
			continue
		}

		return data, nil
	}

	return nil, fmt.Errorf("all replicas failed for chunk %s: %w", hash, lastErr)
}

// nodeAddress looks up the network address for the first available node ID.
func (c *Client) nodeAddress(nodeIDs []string) (string, error) {
	for _, id := range nodeIDs {
		for _, node := range c.config.BlockNodes {
			if node.ID == id {
				return node.Address, nil
			}
		}
	}
	return "", fmt.Errorf("none of the nodes %v found in config", nodeIDs)
}