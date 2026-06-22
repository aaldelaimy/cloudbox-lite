# CloudBox Lite

A distributed file backup system built in Go. Demonstrates backend systems
concepts including chunked file storage, SHA-256 content addressing,
deduplication, consistent hashing, and replication.

## Architecture

The system has three components:

**CLI Client** - splits files into chunks, hashes each chunk, uploads to block
servers, registers metadata, and reconstructs files on download.

**Metadata Server** - tracks filenames, versions, file hashes, and the ordered
chunk list for each file. Backed by SQLite. Does not store file bytes.

**Block Storage Nodes** - store raw chunk bytes on disk, addressed by hash.
Multiple nodes run on different ports to simulate a distributed cluster.

```
CLI Client
    │
    ├── POST /files ──────────────────► Metadata Server (localhost:8080)
    │
    └── PUT /chunks/{hash} ──────────► Block Node 1 (localhost:9001)
                                       Block Node 2 (localhost:9002)
                                       Block Node 3 (localhost:9003)
```

## Features

- Chunk-based file storage with configurable chunk size
- SHA-256 content addressing - chunks identified by their hash
- Chunk-level deduplication - identical chunks stored once across all files
- File versioning - re-uploading a changed file creates a new version
- Consistent hashing - chunks distributed across nodes using a hash ring
- Replication - each chunk stored on multiple nodes for fault tolerance
- Failed-node recovery - downloads try replica nodes if primary is unavailable
- Hash verification - downloaded files verified against original file hash
- Unit and integration tests

## Project Structure

```
cloudbox-lite/
  cmd/
    cloudbox/          # CLI entry point
    metadata-server/   # metadata server entry point
    block-server/      # block storage node entry point
  internal/
    chunker/           # file chunking
    hashing/           # SHA-256 helpers
    client/            # upload/download logic and HTTP client
    metadata/          # metadata models, SQLite store, HTTP handlers
    blockstore/        # chunk storage and HTTP handlers
    hashring/          # consistent hash ring
  tests/
    integration/       # end-to-end tests
  docs/
    design.md          # system architecture and tradeoffs
    notes.md           # phase-by-phase notes
    interview-notes.md # deeper explanations of major components
```
## More Details

- [Design](docs/design.md) - architecture, flows, schema, and tradeoffs
- [Build Notes](docs/notes.md) - phase-by-phase build notes
- [System Explanation](docs/system-explanations.md) - plain-English system explanation

## Setup

```bash
git clone https://github.com/yourname/cloudbox-lite
cd cloudbox-lite
go mod tidy
```

Create data directories:

```bash
mkdir -p data/metadata
mkdir -p data/nodes/node1/chunks
mkdir -p data/nodes/node2/chunks
mkdir -p data/nodes/node3/chunks
```

## Running the System

Open four terminal windows.

**Metadata server:**
```bash
go run cmd/metadata-server/main.go
```

**Block nodes:**
```bash
go run cmd/block-server/main.go -port 9001 -data ./data/nodes/node1/chunks
go run cmd/block-server/main.go -port 9002 -data ./data/nodes/node2/chunks
go run cmd/block-server/main.go -port 9003 -data ./data/nodes/node3/chunks
```

## Commands

```bash
# upload a file
go run cmd/cloudbox/main.go upload ./resume.pdf

# download a file
go run cmd/cloudbox/main.go download resume.pdf ./restored_resume.pdf

# list all uploaded files
go run cmd/cloudbox/main.go list

# inspect file metadata and chunk locations
go run cmd/cloudbox/main.go inspect resume.pdf

# check server health
go run cmd/cloudbox/main.go status
```

## Demo

**Upload a file:**
```
$ go run cmd/cloudbox/main.go upload ./go.mod
Uploaded go.mod
File size:                342 bytes
Chunks:                   1
New chunks stored:        1
Duplicate chunks skipped: 0
Version:                  1
File hash:                a3f9c2...
```

**Upload the same file again (deduplication):**
```
$ go run cmd/cloudbox/main.go upload ./go.mod
Uploaded go.mod
File size:                342 bytes
Chunks:                   1
New chunks stored:        0
Duplicate chunks skipped: 1
Version:                  1
File hash:                a3f9c2...
```

**Download and verify:**
```
$ go run cmd/cloudbox/main.go download go.mod ./restored_go.mod
Downloaded go.mod to ./restored_go.mod
File size:     342 bytes
Chunks:        1
File hash:     a3f9c2...
Hash verified: ✓
```

**Node failure recovery:**
```
# stop node-2, then:
$ go run cmd/cloudbox/main.go download go.mod ./restored_go.mod
Downloaded go.mod to ./restored_go.mod
Hash verified: ✓
# succeeds because replicas exist on other nodes
```

**Check status:**
```
$ go run cmd/cloudbox/main.go status
Metadata server: localhost:8080 healthy ✓
Storage nodes:
  node-1     localhost:9001 healthy ✓
  node-2     localhost:9002 UNREACHABLE ✗
  node-3     localhost:9003 healthy ✓
```

## Running Tests

```bash
# all tests
go test ./...

# verbose
go test -v ./...

# one package
go test ./internal/chunker/...
```

## Design Tradeoffs

**Fixed-size chunking vs content-defined chunking**
We use fixed-size chunks (32 KB default) for simplicity. Content-defined
chunking (rolling hash) would handle insertions better but adds complexity.

**SQLite vs distributed database**
SQLite is sufficient for a local simulation. A production system would need
a distributed metadata store with replication and consensus.

**Round-robin vs consistent hashing**
We started with round-robin and replaced it with consistent hashing so that
adding or removing nodes only remaps a minimal number of chunks.

**Local filesystem vs cloud object storage**
Chunks are stored as files on local disk named by their hash. In production
these would live in S3 or similar object storage.

## Limitations

- Single metadata server with no replication or failover
- No re-replication when a node is removed permanently
- No authentication or encryption
- Local simulation only - not production-scale
- No background health monitoring

## Potential Improvements

- Raft-based metadata replication for fault tolerance
- Background re-replication when nodes go down
- Streaming uploads for very large files
- Authentication and TLS
- Docker Compose setup for easier local demo