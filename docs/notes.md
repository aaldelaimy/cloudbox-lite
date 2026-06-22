# CloudBox Lite - Notes

## Phase 0

- A file is a sequence of bytes. Go reads them as []byte.
- Chunking splits the byte sequence into fixed-size pieces.
- Chunk order is critical. Wrong order = corrupted file.
- SHA-256 produces a 64-character fingerprint of any bytes. Same bytes → same hash, always.
- Content-addressed storage: the hash of the content IS its address.
  Identical content → same hash → stored once. Deduplication falls out naturally.
- Chunk hash: fingerprint of one chunk. Used as the chunk's ID for deduplication.
- Full file hash: fingerprint of the whole file. Used after download to verify the
  reconstructed file is byte-for-byte identical to the original.
- Metadata server: tracks filenames, versions, chunk order, full file hash, node locations.
  Does NOT store raw bytes.
- Block storage nodes: store raw chunk bytes, looked up by hash.
  Know nothing about filenames or ordering.
- This project simulates distributed systems locally. Nodes are HTTP servers on
  different ports. Concepts are real; scale is not.

## Phase 1

- os.Open returns a *os.File which implements io.Reader.
- defer file.Close() releases the file handle when the function exits, even on error.
- io.ReadFull fills the buffer completely, unlike Read which can return short reads.
- io.EOF: reader was already empty when Read was called.
- io.ErrUnexpectedEOF: ReadFull hit end of data before filling the buffer.
  This is normal for the last chunk of a file.
- buf[:n] trims the buffer to only the bytes actually read.
- chunkReader accepts io.Reader so tests can use bytes.NewReader without real files.
  This pattern is called dependency injection.
- fmt.Errorf with %w wraps the original error so callers can inspect it with errors.Is.

## Phase 2: Hashing

- SHA-256 produces a 32-byte digest, always the same length regardless of input size.
- We hex-encode those 32 bytes into a 64-character string for use as IDs and filenames.
- sha256.Sum256(data) hashes a byte slice all at once. Returns [32]byte (fixed array).
- hash[:] converts [32]byte array to []byte slice. Go functions usually want slices.
- sha256.New() creates a streaming hasher. Feed it data incrementally with io.Copy.
  Used for full file hashes so we never load the whole file into memory at once.
- hex.EncodeToString converts raw bytes to a readable hex string.
- HashBytes is used for chunk hashes (chunks are small, fine to pass all at once).
- hashReader is used for full file hashes (file could be large, stream it through).
- Same io.Reader pattern as chunker - hashReader takes io.Reader so tests work
  without real files.

## Phase 3: Block Storage Server

- An HTTP handler in Go is just a function: func(w http.ResponseWriter, r *http.Request)
- r is the incoming request. w is how you write the response.
- r.Method tells you GET, PUT, HEAD, etc.
- Status codes: 200 OK, 201 Created, 400 Bad Request, 404 Not Found, 500 Server Error.
- The server verifies the uploaded bytes actually hash to the URL hash.
  Never trust the client to send correct data.
- Chunks are stored as files named by their hash. No database needed for block nodes.
- ErrChunkNotFound is a named sentinel error so callers can check for it specifically.
- t.TempDir() creates a temp directory that auto-deletes after the test. Clean tests.
- os.MkdirAll creates a directory and all missing parents (like mkdir -p).
- HEAD request: check existence without returning the body. Used for dedup checks.

## Phase 4: Metadata Models

- FileMetadata is the internal representation of a file's metadata.
  It never contains actual file bytes - just what's needed to reconstruct the file.
- ChunkRef.Index is critical. Chunks must be reassembled in index order.
  Wrong order = corrupted file.
- ChunkRef.Nodes is a list of node IDs that have a copy of this chunk.
  Used during download to know where to fetch each chunk from.
- FileHash is the SHA-256 of the entire file. Used after download to verify
  the reconstructed file is correct.
- Struct tags like `json:"filename"` control JSON field names.
  Convention is lowercase in JSON.
- UploadRequest is separate from FileMetadata - one is the HTTP contract,
  the other is the internal representation. Keeps them from being coupled.

## Phase 5: Metadata SQLite Store

- SQLite lives in a single file. No separate server needed.
- ":memory:" creates an in-memory database - used in tests for speed and cleanliness.
- Transactions wrap multiple inserts so they all succeed or all fail together.
  Prevents partial writes that would leave the database in a corrupted state.
- INSERT OR IGNORE skips the insert if the row already exists (by primary key).
  Used for chunks - same chunk can belong to multiple files.
- ORDER BY chunk_index ASC guarantees chunks come back in the right order.
- defer rows.Close() releases database resources, same idea as defer file.Close().
- Versioning logic: same filename + same hash = no new version.
  Same filename + different hash = version + 1.
- The store layer is separate from HTTP handlers. Database logic should never
  be mixed with request/response handling.

## Phase 6: Metadata HTTP Server

- HTTP handlers call the store layer. They never contain database logic themselves.
- json.NewDecoder(r.Body).Decode(&req) parses a JSON request body into a struct.
- json.NewEncoder(w).Encode(v) writes a struct out as a JSON response.
- Go's standard library has no built-in router. We parse URL paths manually
  using strings.TrimPrefix and strings.HasSuffix.
- httptest.NewServer creates a real HTTP server for tests. No mocking needed.
- defer ts.Close() shuts the test server down after each test.
- Validation happens in the handler before calling the store.
  Bad requests return 400. Store errors return 500. Not found returns 404.

## Phase 7: CLI Upload

- http.NewRequest is used for PUT requests. http.Post only supports POST.
- defer resp.Body.Close() releases the network connection after reading.
- io.ReadAll reads the entire response body into memory.
- Chunks are uploaded before metadata is registered. If we did it the other way
  and a chunk upload failed, metadata would point to chunks that don't exist.
- filepath.Base extracts the filename from a full path.
- The client package connects all previous packages together:
  chunker → hashing → blockstore (via HTTP) → metadata server (via HTTP).

## Phase 8: CLI Download

- Download fetches metadata first, then downloads chunks in the order
  the metadata server returns them (already sorted by index).
- os.Create creates or truncates a file for writing.
- outFile.Close() is called explicitly before hashing to flush all writes to disk.
  defer is still there as a safety net but explicit close happens first.
- os.Remove deletes the output file on any failure - chunk download error,
  write error, or hash mismatch. Never leave a corrupted file on disk.
- Hash verification: hash the reconstructed file and compare to the stored
  file hash from metadata. Mismatch means the file is corrupted - hard failure.
- The download flow is the mirror of upload:
  upload:   chunk → hash → store chunks → register metadata
  download: fetch metadata → fetch chunks in order → write → verify hash

## Phase 9: List, Inspect, Status

- List returns a summary of all files at their latest versions.
- Inspect returns full metadata for one file - all chunk hashes and node locations.
  Useful for debugging: you can see exactly where every chunk lives.
- Status hits the /health endpoint on every server and reports which are reachable.
  This is how you detect a node is down before attempting a download.
- CheckHealth returns a bool - healthy or not. Simple and unambiguous.
- %-30s in format strings left-aligns and pads output to fixed width columns.
- Getters expose private fields without making them public.

## Phase 10: Multiple Block Nodes

- Round-robin: chunk.index % len(nodes) distributes chunks evenly across nodes.
- Weakness of round-robin: adding or removing a node reshuffles all assignments.
  This is why consistent hashing exists - Phase 11 fixes this.
- nodeAddress separates node ID from node address. The metadata stores IDs,
  the config maps IDs to addresses. Changing an address doesn't affect metadata.
- Running multiple block servers means just running the same binary on different
  ports with different data directories. Each is a completely independent process.

## Phase 11: Consistent Hashing

- hash % numNodes is bad because adding/removing a node reshuffles almost everything.
- A hash ring places nodes at positions on a circle. A chunk's position determines
  its node by walking clockwise to the next node.
- Adding a node only affects chunks between the new node and its predecessor.
  Everything else stays put.
- Virtual nodes: each physical node gets multiple positions on the ring.
  More positions = more even distribution.
- hashKey converts a string to uint32 by taking the first 4 bytes of SHA-256.
- sort.Search does binary search to find the clockwise node in O(log n).
- sync.RWMutex allows concurrent reads but exclusive writes.
- We use the chunk content hash as the ring key, not the chunk index.
  Same content always maps to the same node.
- TestRemappingOnNodeRemoval verifies the core consistent hashing guarantee:
  only chunks on the removed node move. Everything else stays.

## Phase 12: Deduplication

- Before uploading a chunk, send HEAD /chunks/{hash} to check if it exists.
- HEAD returns status code only, no body. Cheaper than PUT for existence checks.
- If HEAD returns 200, skip the PUT entirely. Count as duplicate.
- Content-addressed storage makes dedup automatic - same content = same hash =
  same node = same check. No extra coordination needed.
- Dedup works at the chunk level, not the file level. Two completely different
  files can share chunks if they have overlapping content.
- The dedup test uploads the same file twice and verifies the second upload
  stores zero new chunks.

## Phase 13: Replication

- Replication factor N means each chunk is stored on N distinct nodes.
- GetNodes(key, N) returns N nodes walking clockwise from the chunk's position.
- Upload stores the chunk on all N nodes and records all node IDs in metadata.
- If one node fails during upload, we warn and continue. Partial replication
  is better than failing the entire upload.
- If zero nodes succeed, the upload fails.
- downloadChunkWithFallback tries each replica in order. First success wins.
- Only fails if every replica for a chunk is unavailable.
- Replicas are clockwise neighbors on the ring - consistent with placement logic.
- The metadata Nodes field now contains multiple IDs per chunk instead of one.

## Phase 14: Failure Handling

- downloadChunkWithFallback tries each replica in order. First success wins.
- If one node is down, the next replica is tried transparently.
- If all replicas are down, the download fails with a clear error.
- The output file is deleted on any failure - never leave partial files on disk.
- Integration tests spin up real servers and test the full stack end to end.
- TestNodeFailureWithReplication proves failure recovery works by shutting down
  a server mid-test and verifying the download still succeeds.
- TestAllReplicasFailedDownload proves the system fails clearly when all nodes
  are unavailable, and cleans up the output file.
- Difference between unit tests and integration tests:
  unit = one package in isolation
  integration = multiple packages wired together, full flow