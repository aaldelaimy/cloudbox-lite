package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cloudbox-lite/internal/blockstore"
	"cloudbox-lite/internal/client"
	"cloudbox-lite/internal/metadata"
	"encoding/json"
)

// testCluster holds all the servers needed for an integration test.
type testCluster struct {
	metaServer   *httptest.Server
	blockServers []*httptest.Server
	client       *client.Client
}

func newTestCluster(t *testing.T, replicationFactor int) *testCluster {
	t.Helper()

	// start metadata server
	metaServer := newIntegrationMetadataServer(t)

	// start 3 block servers
	var blockServers []*httptest.Server
	var nodeConfigs []client.NodeConfig

	for i := 0; i < 3; i++ {
		bs := newIntegrationBlockServer(t)
		blockServers = append(blockServers, bs)
		nodeConfigs = append(nodeConfigs, client.NodeConfig{
			ID:      nodeID(i),
			Address: bs.URL[7:],
		})
	}

	config := client.Config{
		MetadataAddr:      metaServer.URL[7:],
		BlockAddr:         blockServers[0].URL[7:],
		BlockNodes:        nodeConfigs,
		ReplicationFactor: replicationFactor,
	}

	c := client.NewClient(config)

	return &testCluster{
		metaServer:   metaServer,
		blockServers: blockServers,
		client:       c,
	}
}

func (tc *testCluster) close() {
	tc.metaServer.Close()
	for _, bs := range tc.blockServers {
		bs.Close()
	}
}

func nodeID(i int) string {
	return []string{"node-1", "node-2", "node-3"}[i]
}

func newIntegrationBlockServer(t *testing.T) *httptest.Server {
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

func newIntegrationMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "metadata.db")
	store, err := metadata.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create metadata store: %v", err)
	}
	server := metadata.NewServer(store)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return httptest.NewServer(mux)
}

func TestUploadDownloadCorrectness(t *testing.T) {
	cluster := newTestCluster(t, 1)
	defer cluster.close()

	// create a temp file to upload
	tmpFile := filepath.Join(t.TempDir(), "testfile.txt")
	content := []byte("the quick brown fox jumps over the lazy dog")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// upload
	_, err := cluster.client.Upload(tmpFile, 16)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// download
	outputPath := filepath.Join(t.TempDir(), "restored.txt")
	_, err = cluster.client.Download("testfile.txt", outputPath)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	// verify bytes match
	restored, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restored) != string(content) {
		t.Errorf("restored file does not match original\nwant: %q\ngot:  %q", content, restored)
	}
}

func TestNodeFailureWithReplication(t *testing.T) {
	cluster := newTestCluster(t, 2)
	defer cluster.close()

	// create a temp file to upload
	tmpFile := filepath.Join(t.TempDir(), "testfile.txt")
	content := []byte("this file should survive a node failure")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// upload with replication factor 2
	_, err := cluster.client.Upload(tmpFile, 16)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// shut down one block server to simulate failure
	cluster.blockServers[0].Close()

	// download should still succeed because replicas exist
	outputPath := filepath.Join(t.TempDir(), "restored.txt")
	_, err = cluster.client.Download("testfile.txt", outputPath)
	if err != nil {
		t.Fatalf("download failed after node failure: %v\nthis means replication is not working correctly", err)
	}

	// verify the content is correct
	restored, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(restored) != string(content) {
		t.Errorf("restored file does not match original after node failure\nwant: %q\ngot:  %q", content, restored)
	}
}

func TestAllReplicasFailedDownload(t *testing.T) {
	cluster := newTestCluster(t, 1)
	defer cluster.close()

	// create and upload a file
	tmpFile := filepath.Join(t.TempDir(), "testfile.txt")
	content := []byte("this file will lose all its replicas")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := cluster.client.Upload(tmpFile, 16)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// shut down ALL block servers
	for _, bs := range cluster.blockServers {
		bs.Close()
	}

	// download should fail clearly
	outputPath := filepath.Join(t.TempDir(), "restored.txt")
	_, err = cluster.client.Download("testfile.txt", outputPath)
	if err == nil {
		t.Error("expected download to fail when all nodes are down, but it succeeded")
	}

	// the output file should not exist
	if _, statErr := os.Stat(outputPath); statErr == nil {
		t.Error("expected output file to be deleted after failed download")
	}
}

func TestDedupAcrossFiles(t *testing.T) {
	cluster := newTestCluster(t, 1)
	defer cluster.close()

	// create two files with the same content
	dir := t.TempDir()
	content := []byte("shared content between two files")

	file1 := filepath.Join(dir, "file1.txt")
	file2 := filepath.Join(dir, "file2.txt")

	os.WriteFile(file1, content, 0644)
	os.WriteFile(file2, content, 0644)

	summary1, err := cluster.client.Upload(file1, 16)
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}

	summary2, err := cluster.client.Upload(file2, 16)
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}

	if summary1.NewChunks == 0 {
		t.Error("expected first upload to store new chunks")
	}

	if summary2.DuplicateChunks != summary1.ChunkCount {
		t.Errorf("expected second upload to find all chunks as duplicates, got %d duplicates out of %d chunks",
			summary2.DuplicateChunks, summary1.ChunkCount)
	}
}

// minimal in-memory metadata server for integration tests
// reuses the real metadata package
var _ = json.Marshal // ensure encoding/json is used
var _ = sync.Mutex{}