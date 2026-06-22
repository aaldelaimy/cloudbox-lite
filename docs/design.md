# CloudBox Lite - Design

## Overview

CloudBox Lite simulates a distributed file backup system locally. It is not
production software. It demonstrates backend systems concepts that appear in
real distributed storage systems like GFS, HDFS, and S3.

## Upload Flow

1. CLI hashes the full file (SHA-256) - stored later for download verification
2. CLI splits the file into fixed-size chunks (default 32 KB)
3. CLI hashes each chunk - the hash is its content address and storage ID
4. For each chunk:
   - Use consistent hashing to find N nodes (N = replication factor)
   - Send HEAD /chunks/{hash} to check if chunk already exists
   - If exists: skip upload, count as duplicate
   - If missing: send PUT /chunks/{hash} with raw bytes
   - Block server verifies the bytes hash to the URL hash before storing
5. After all chunks uploaded: POST /files to metadata server with ordered chunk list
6. CLI prints upload summary

Metadata is registered only after all chunks are safely stored. If chunk
upload fails, no metadata is written, leaving the system in a clean state.

## Download Flow

1. CLI sends GET /files/{filename} to metadata server
2. Metadata server returns ordered chunk list with node locations
3. For each chunk in order:
   - Try primary node first
   - If primary fails, try next replica
   - If all replicas fail, abort and delete output file
4. Write chunks to output file in order
5. Close output file
6. Hash reconstructed file
7. Compare to stored file hash
8. If mismatch: delete output file and return error
9. If match: report success

## Metadata Schema

Four tables in SQLite:

- files: one row per file version (filename, version, size, file_hash)
- chunks: one row per unique chunk (hash, size)
- file_chunks: links files to chunks with ordering (file_id, chunk_hash, chunk_index)
- chunk_locations: tracks which nodes have each chunk (chunk_hash, node_id)

Versioning rules:
- New filename → version 1
- Same filename, same file hash → no new version
- Same filename, different file hash → version + 1

## Consistent Hashing

Nodes are placed on a uint32 number line (0 to 4,294,967,295) by hashing
their ID. Each physical node gets 100 virtual positions for even distribution.

To assign a chunk to a node: hash the chunk, find its uint32 position, walk
clockwise to the first virtual node, use its physical node.

Adding a node: only chunks between the new node and its predecessor move.
Removing a node: only chunks on that node need to move to the next node.

This is fundamentally better than hash % numNodes where any change
reshuffles almost everything.

## Replication

Replication factor N means each chunk is stored on N distinct nodes.
GetNodes walks clockwise collecting N distinct physical nodes.

During upload: chunk is uploaded to all N nodes. Node IDs stored in metadata.
During download: nodes tried in order. First success wins.

If a node fails permanently, chunks on that node become under-replicated.
CloudBox Lite does not re-replicate automatically. A production system would
run a background process to detect and fix under-replicated chunks.

## Block Storage Design

Each block node stores chunks as files on local disk:

```
data/nodes/node1/chunks/a82f9c...
data/nodes/node2/chunks/b19de4...
```

The chunk hash is the filename. No database needed.

On PUT: server hashes the received bytes and compares to the URL hash.
Rejects if mismatch. This prevents storing corrupted data.

On GET: reads the file by hash and returns raw bytes.
On HEAD: checks if the file exists without reading it.

## What This Project Simulates vs Real Systems

Real concepts implemented here:
- Content-addressed storage (same as Git, IPFS)
- Metadata/data plane separation (same as GFS, HDFS)
- Consistent hashing (same idea as Dynamo, Cassandra)
- Chunk-level deduplication
- Replication for fault tolerance
- Hash-based integrity verification

What is simulated locally:
- Nodes are HTTP servers on different ports on one machine
- No real network partitions
- No distributed consensus
- SQLite instead of a distributed database
- Local filesystem instead of cloud object storage