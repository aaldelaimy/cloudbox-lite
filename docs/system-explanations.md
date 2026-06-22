# CloudBox Lite - System Explanations

## Phase 0: Core Concepts

A file is just a sequence of bytes stored on disk. Your program doesn't care
if it's a PDF or an image - it just sees numbers between 0 and 255. In Go,
that's a []byte.

Chunking means splitting those bytes into fixed-size pieces instead of treating
the whole file as one blob. If a file is 100 bytes and your chunk size is 32,
you get three chunks of 32 bytes and one final chunk of 4 bytes. The last chunk
is almost always smaller than the rest, and that's fine.

Chunk order matters because a file is bytes in a specific sequence. If you
reassemble the chunks in the wrong order, the bytes are all there but in the
wrong positions. The file is corrupted. This is why every chunk carries an
index - its position in the original file.

SHA-256 is a hash function. It takes any amount of bytes and produces a
fixed 64-character fingerprint. Same input always produces the same output.
Different input produces a completely different output. We use it to give
each chunk a unique ID based purely on its content.

Content-addressed storage means the hash of the content is its address.
Instead of naming things with arbitrary IDs, you let the content name itself.
Two chunks with identical bytes always get the same hash, which means they
automatically get the same address. Deduplication falls out of this naturally
- if the address already exists, the content is already stored.

The chunk hash identifies one chunk and is used during upload to detect
duplicates. The full file hash is the SHA-256 of the entire file before
chunking. It gets stored in metadata and used after download to verify the
reconstructed file is byte-for-byte identical to the original.

The metadata server and block storage nodes are separate because they have
different jobs. The metadata server knows what files exist, what chunks they
need, in what order, and where those chunks live. The block nodes just store
raw bytes and return them when asked. Neither can work without the other.
This separation mirrors how real distributed storage systems like GFS and
HDFS are designed.

This project simulates all of this locally. The nodes are just HTTP servers
running on different ports on your machine. The concepts are real. The scale
is not, and you should be honest about that in any interview.

---

## Phase 1: Chunking

The chunking package opens a file and splits it into fixed-size byte slices.
Each chunk is a struct with three fields: its index (where it belongs in the
file), its data (the raw bytes), and its size (how many bytes it actually
contains).

The key function is ChunkFile which takes a file path and a chunk size.
It opens the file, then calls chunkReader which does the actual work.
chunkReader accepts an io.Reader instead of a file path - this is the
important design decision. Because io.Reader is an interface that both
real files and in-memory byte slices implement, tests can pass bytes
directly without needing files on disk. This pattern is called dependency
injection.

Inside chunkReader we use io.ReadFull instead of the regular Read. The
difference matters: Read is allowed to return fewer bytes than you asked
for even if more are available. io.ReadFull keeps reading until the buffer
is completely full or it runs out of data. For chunking you want full
chunks, so io.ReadFull is correct.

When io.ReadFull hits the end of the file it returns io.ErrUnexpectedEOF -
meaning it ran out of data before filling the buffer. This is the normal
case for the last chunk of any file that doesn't divide evenly by the
chunk size. We treat this as done, not an error. We also slice the buffer
to buf[:n] so we only store the bytes that were actually read, not the
zero-filled remainder of the buffer.

defer file.Close() ensures the file handle is released when the function
exits, even if it exits early due to an error.

---

## Phase 2: Hashing

The hashing package provides two functions. HashBytes takes a byte slice
and returns a 64-character hex string. HashFile opens a file and hashes
its entire contents, also returning a hex string.

SHA-256 produces 32 raw bytes. We convert those to a hex string because
64 hex characters are easy to store, compare, print, and use as filenames.

HashBytes uses sha256.Sum256 which hashes everything at once. This is fine
for chunks because chunks are small - 32 KB at most.

HashFile uses sha256.New to create a streaming hasher, then io.Copy to
feed the file through it incrementally. This means we never load the entire
file into memory. For a 10 GB file, this is the only viable approach.

The same io.Reader pattern from the chunker applies here. The internal
hashReader function takes an io.Reader so tests can pass bytes directly
without real files.

The chunk hash and the full file hash serve different purposes. The chunk
hash is the chunk's identity - used during upload to detect duplicates and
choose storage nodes. The full file hash is a correctness guarantee - used
after download to verify the reconstructed file matches the original exactly.

---

## Phase 3: Block Storage Server

The block storage server stores and retrieves raw chunk bytes on disk.
It is the simplest component in the system. Each chunk is saved as a file
named by its hash. No database needed - the filesystem is the database.

It exposes four endpoints. PUT /chunks/{hash} stores a chunk. The server
reads the request body, hashes the bytes itself, and compares that hash
to the one in the URL. If they don't match, the request is rejected. The
server never trusts the client to send correct data. If the chunk already
exists, it returns duplicate true without writing again. If it's new, it
writes the file and returns stored true.

GET /chunks/{hash} reads the file from disk and returns the raw bytes.
HEAD /chunks/{hash} checks if the file exists and returns 200 or 404
without sending any bytes back. This is used during deduplication checks -
you want to know if a chunk exists without the overhead of downloading it.
GET /health just returns healthy so the CLI can check if the node is alive.

In tests, t.TempDir() creates a temporary directory that gets automatically
deleted when the test finishes. ErrChunkNotFound is a named sentinel error
so callers can check for that specific error type using errors.Is.

---

## Phase 4: Metadata Models

The metadata models are just data structures. No logic, no database, no HTTP.
They define the shape of the data that flows through the system.

FileMetadata is the internal representation of a file. It holds the filename,
version number, total size, full file hash, and an ordered slice of ChunkRefs.
It never holds actual file bytes.

ChunkRef describes one chunk within a file. Its Index field is critical -
chunks must be reassembled in index order on download. Its Hash is the
content address used to fetch the chunk from block storage. Its Nodes field
is a list of node IDs that have a copy of this chunk.

UploadRequest and UploadResponse are the HTTP contract for the metadata
server's POST /files endpoint. They are separate from FileMetadata because
the HTTP API and the internal representation can evolve independently.

Struct tags like json:"filename" control how Go's JSON encoder names fields
in the output. Without them, Go would use the capitalized field names which
doesn't match JSON conventions.

---

## Phase 5: Metadata SQLite Store

The metadata store is the database layer. It uses SQLite because SQLite
lives in a single file with no separate server required.

Four tables. files stores one row per file version - filename, version,
size, and full file hash. chunks stores one row per unique chunk - just
hash and size. The same chunk can appear in many files but only gets one
row here. file_chunks is the link table - it says file 3 uses chunk h1
at index 0, chunk h2 at index 1. chunk_locations says chunk h1 lives on
node-1.

Versioning works in three cases. New filename creates version 1. Same
filename with the same file hash does nothing - identical content, no
new version needed. Same filename with a different file hash creates
version plus one.

Every save operation runs inside a transaction. That means all the inserts
either all succeed or all get rolled back. Without a transaction, a crash
partway through could leave a file row with no chunks, or chunks with no
locations - corrupted state.

SQLite with one connection can't have two open query cursors simultaneously.
When we load chunks for a file and then try to load node locations for each
chunk inside the same loop, the second query deadlocks waiting for the first
cursor to close. The fix is to close the first cursor explicitly before
starting the node location queries.

In tests, :memory: creates the database entirely in RAM. Nothing written
to disk, starts fresh every test, automatically gone when the test finishes.

---

## Phase 6: Metadata HTTP Server

The HTTP server is the layer that exposes the store over the network.
The store handles SQL. The server handles requests and responses.
They never mix - database logic stays in the store, HTTP logic stays
in the handlers.

POST /files receives the file metadata from the CLI after chunks are
uploaded. It decodes the JSON body, validates the required fields, calls
the store to save it, and returns the filename and version number.

GET /files/{filename} returns the full metadata for one file including
the ordered chunk list with node locations. This is what the CLI calls
during download to find out what chunks to fetch and where they are.

GET /files returns a summary of all files at their latest versions.
GET /files/{filename}/inspect returns the same as the plain GET but
is intended for detailed debugging output. GET /health checks liveness.

Because Go's standard library has no built-in URL router, we parse paths
manually. Strip the /files/ prefix, check if the remainder ends with
/inspect, extract the filename from what's left.

Tests use httptest.NewServer which creates a real HTTP server on a random
local port. Tests send actual HTTP requests to it. This means real JSON
encoding, real status codes, real routing all get tested. When the test
finishes, defer ts.Close() shuts the server down.

---

## Phase 7: CLI Upload

The upload command is where all the previous packages connect for the
first time. Before this, every package existed in isolation.

When you run cloudbox upload ./resume.pdf, five things happen in order.

First, the CLI hashes the entire file to get the full file hash. This
gets stored in metadata later and used after download to verify correctness.

Second, the CLI chunks the file using the chunker package. Each chunk
comes back as raw bytes with an index. The CLI immediately hashes each
chunk's bytes to get its content address.

Third, the CLI uses the hash ring to find which node should store each
chunk, then uploads each chunk with a PUT request. The URL is
/chunks/{hash}. The block server receives the bytes, verifies the hash,
and stores the chunk. Before uploading, the CLI sends a HEAD request to
check if the chunk already exists - if it does, the upload is skipped.

Fourth, once every chunk is safely stored, the CLI sends the file metadata
to the metadata server with a POST request. Metadata is registered only
after all chunks are uploaded. If a chunk upload fails, no metadata is
written, leaving the system in a clean state.

Fifth, the CLI prints a summary showing filename, size, chunk count, new
chunks, duplicates skipped, version, and file hash.

For PUT requests we use http.NewRequest because http.Post only supports POST.
defer resp.Body.Close() releases the network connection after each request.

---

## Phase 8: CLI Download

The download command is the mirror of upload. Upload breaks a file apart
and stores the pieces. Download retrieves the pieces and reconstructs the
file, then proves the result is correct.

When you run cloudbox download resume.pdf ./restored_resume.pdf, six
things happen.

First, the CLI asks the metadata server for the file's metadata. This
returns the full file hash and the ordered chunk list with node locations.
Without this, the CLI has no idea what chunks make up the file.

Second, the CLI creates the output file on disk.

Third, the CLI loops through the chunk list in order - the metadata server
already returns chunks sorted by index - and downloads each chunk from its
node. Each chunk gets written to the output file immediately.

Fourth, if any chunk fails to download or fails to write, the output file
is deleted and the operation fails. We never leave a partial file on disk.
The user either gets a correct file or no file at all.

Fifth, the output file is closed explicitly before hashing. Closing flushes
any buffered writes to disk. Hashing before closing could hash an incomplete
file.

Sixth, the CLI hashes the reconstructed file and compares it to the file
hash stored in metadata. If they match, the file is byte-for-byte identical
to the original. If they don't match, the output file is deleted and the
operation fails hard. We never silently hand the user a broken file.

---

## Phase 9: List, Inspect, Status

Three read-only commands that give visibility into the system without
changing anything.

cloudbox list calls GET /files on the metadata server and prints a table
of all files at their latest versions - filename, version, size, chunk count.

cloudbox inspect calls GET /files/{filename}/inspect and prints the full
metadata for one file including every chunk's hash, size, and node locations.
This is a debugging tool. If a download fails, you can inspect the file and
see exactly where each chunk should be.

cloudbox status hits the /health endpoint on the metadata server and every
block node. It prints whether each one responded with 200 or failed to
connect. This is how you detect a node is down before attempting an upload
or download.

CheckHealth is intentionally simple. It makes one GET request and returns
true or false. No retries, no timeouts beyond Go's defaults. Either the
server answered or it didn't.

NodeConfig pairs a node's logical ID with its network address. The ID is
the stable identifier stored in metadata. The address is where it's running
right now. They're separate so changing an address doesn't require updating
any stored metadata.

---

## Phase 10: Multiple Block Nodes

Before this phase there was one block node. Now there are three, each
running as an independent HTTP server on a different port with its own
data directory.

Chunks are distributed across nodes using round-robin - chunk index modulo
number of nodes. Chunk 0 goes to node-1, chunk 1 to node-2, chunk 2 to
node-3, chunk 3 back to node-1.

The weakness of round-robin is that adding or removing a node reshuffles
every assignment. Chunk 0 was on node-1, now it might be on node-2. In a
real system with terabytes of data you'd have to move almost everything.
This is exactly the problem consistent hashing solves in Phase 11.

During download, the metadata tells us which node ID has each chunk. We
look up that node ID in our config to find its network address. This
separation matters - if you move a node to a different port, you update
the config and nothing else changes.

---

## Phase 11: Consistent Hashing

Consistent hashing solves the reshuffling problem of round-robin.

Imagine a circle with positions 0 to 4,294,967,295. You place each node
somewhere on that circle by hashing its ID to a uint32. To assign a chunk
to a node, you hash the chunk to a uint32, find that position on the circle,
and walk clockwise until you hit a node. That node owns the chunk.

When you add a new node, it only takes ownership of chunks that fall between
it and its predecessor. Everything else stays exactly where it was. When you
remove a node, only its chunks need to move to the next node clockwise.
Compare this to round-robin where any change reshuffles almost everything.

Virtual nodes address uneven distribution. If you place each physical node
at only one position, some nodes might own large arcs and get most of the
chunks while others get very little. By giving each physical node 100
positions spread around the ring, the distribution becomes much more even.

Finding the right node uses binary search on the sorted list of virtual node
positions - O(log n) instead of scanning every position linearly.

We use the chunk's content hash as the ring key, not the chunk index. This
means identical content always maps to the same node regardless of which
file it came from. Deduplication and consistent hashing work together
naturally.

The most important test is TestRemappingOnNodeRemoval. It records where
100 chunks land before removing a node, then checks that none of the chunks
that were NOT on that node moved afterward. If any moved, consistent hashing
is broken.

---

## Phase 12: Deduplication

Before this phase the client uploaded every chunk every time. Now it checks
first.

Before uploading any chunk, the client sends a HEAD request to the block
server. HEAD returns only a status code, no body. If the server returns 200,
the chunk already exists and we skip the PUT entirely. If it returns 404,
the chunk is new and we proceed with the upload.

This works automatically because of content-addressed storage. The chunk's
hash is determined by its content. Same content means same hash, which means
the same position on the hash ring, which means the same block server. The
existence check always goes to exactly the right place with no extra
coordination.

Deduplication works at the chunk level, not the file level. Two completely
different files can share chunks if they have overlapping content - a common
header, repeated sections, anything identical at the byte level. Those shared
chunks are stored once no matter how many files reference them.

The dedup test uploads the same file twice. The first upload stores all
chunks as new. The second upload finds every chunk already exists and stores
zero new chunks. If that holds, deduplication is working.

---

## Phase 13: Replication

Each chunk now gets stored on multiple nodes instead of one. The number of
copies is the replication factor.

The hash ring already supports this. GetNodes returns N distinct nodes
walking clockwise from the chunk's position - the primary node first, then
replicas in order. During upload, the chunk gets uploaded to every node in
that list. All node IDs get stored in the metadata for that chunk.

If one node fails during upload, we log a warning and continue. Partial
replication - the chunk on fewer nodes than intended - is better than
failing the entire upload. But if zero nodes succeed, the upload fails.

During download, downloadChunkWithFallback tries each node ID in the chunk's
node list in order. The first node that returns the chunk wins. If one node
is down, the next replica is tried transparently. Only if every replica for
a chunk is unavailable does the download fail.

One important limitation: CloudBox Lite does not re-replicate when a node
goes down permanently. If node-2 dies and chunks were stored on node-1 and
node-2, those chunks are now under-replicated - only one copy remains on
node-1. We never detect this and never fix it. A production system like
HDFS would run a background process that detects under-replicated blocks
and copies them to healthy nodes to restore the replication factor.

---

## Phase 14: Failure Handling

The failure handling is mostly implemented by Phase 13's
downloadChunkWithFallback. This phase adds proper tests that prove it works.

The integration tests spin up real servers - real block servers backed by
temp directories, a real metadata server backed by a temp SQLite database -
and run the full upload and download flow end to end. These are different
from unit tests which test one package in isolation.

TestUploadDownloadCorrectness uploads a file and downloads it back, then
compares the bytes to verify the reconstructed file is identical to the
original.

TestNodeFailureWithReplication uploads a file with replication factor 2,
shuts down one block server mid-test, then downloads and verifies the file
came back correctly. This directly proves the fallback logic works.

TestAllReplicasFailedDownload shuts down every block server and verifies
the download fails with a clear error and no output file left on disk.
You never want a partial corrupted file left behind.

TestDedupAcrossFiles uploads two files with identical content and verifies
the second upload finds all chunks as duplicates.

---

## Full System Summary

CloudBox Lite is a distributed file backup system built in Go. It has three
components: a CLI client, a metadata server, and multiple block storage nodes.

When you upload a file, the CLI first hashes the entire file to get a
fingerprint for later verification. It then splits the file into fixed-size
chunks and hashes each chunk to get its content address. For each chunk,
the CLI uses a consistent hash ring to determine which node should store
it, checks if that chunk already exists using a HEAD request, and uploads
it if it doesn't. Once all chunks are safely stored, the CLI registers the
file metadata with the metadata server - the filename, version, full file
hash, and the ordered list of chunk hashes with their node locations. Metadata
is always registered last so it never points to chunks that don't exist.

When you download a file, the CLI asks the metadata server for the file's
metadata. The metadata server returns the ordered chunk list with node
locations. The CLI downloads each chunk in order, trying replica nodes if
the primary is unavailable. Once all chunks are written, the output file is
closed and hashed. If the hash matches the one stored during upload, the
file is correct. If not, the output file is deleted and the operation fails.
The system never silently produces a corrupted file.

The metadata server tracks what files exist and how to reconstruct them,
but never stores actual file bytes. The block nodes store raw bytes and
know nothing about filenames or ordering. This separation mirrors how real
distributed storage systems like GFS and HDFS are designed - a metadata
plane and a data plane with clearly separate responsibilities.

Consistent hashing distributes chunks across nodes so that adding or removing
a node only remaps a minimal number of chunks instead of reshuffling
everything. Each chunk is stored on multiple nodes so downloads can survive
a node failure. Chunk-level deduplication means identical content is stored
once regardless of how many files reference it. File versioning means
re-uploading a changed file creates a new version without losing the old one.

The honest limitations are that this runs locally - nodes are HTTP servers
on different ports, not separate machines. The metadata server has no
replication or failover. There is no background re-replication when nodes
go down permanently. There is no authentication or encryption. These are the
right tradeoffs for a portfolio project demonstrating backend systems concepts.
They would all need to be addressed before this could be called production software.