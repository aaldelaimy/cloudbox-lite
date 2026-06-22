package metadata

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store manages file metadata in a SQLite database.
type Store struct {
	db *sql.DB
}

// NewStore opens the SQLite database at the given path and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Limit to one connection so in-memory SQLite shares the same database
	// across all queries. Also prevents deadlocks from nested cursors.
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// migrate creates the tables if they do not already exist.
// Each statement is run separately because SQLite does not support
// multiple statements in a single Exec call reliably.
func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			filename   TEXT NOT NULL,
			version    INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			file_hash  TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(filename, version)
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			hash       TEXT PRIMARY KEY,
			size_bytes INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS file_chunks (
			file_id     INTEGER NOT NULL,
			chunk_hash  TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			PRIMARY KEY (file_id, chunk_index),
			FOREIGN KEY (file_id)    REFERENCES files(id),
			FOREIGN KEY (chunk_hash) REFERENCES chunks(hash)
		)`,
		`CREATE TABLE IF NOT EXISTS chunk_locations (
			chunk_hash TEXT NOT NULL,
			node_id    TEXT NOT NULL,
			PRIMARY KEY (chunk_hash, node_id),
			FOREIGN KEY (chunk_hash) REFERENCES chunks(hash)
		)`,
	}

	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// SaveFile saves file metadata to the database.
// If the filename already exists with the same file hash, it does nothing.
// If the filename exists with a different hash, it creates a new version.
// If the filename is new, it creates version 1.
func (s *Store) SaveFile(req UploadRequest) (*UploadResponse, error) {
	existing, err := s.getLatestFile(req.Filename)

	if err == nil {
		if existing.FileHash == req.FileHash {
			return &UploadResponse{
				Filename: existing.Filename,
				Version:  existing.Version,
			}, nil
		}
	}

	version := 1
	if err == nil {
		version = existing.Version + 1
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO files (filename, version, size_bytes, file_hash) VALUES (?, ?, ?, ?)`,
		req.Filename, version, req.SizeBytes, req.FileHash,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert file: %w", err)
	}

	fileID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get file ID: %w", err)
	}

	for _, chunk := range req.Chunks {
		_, err = tx.Exec(
			`INSERT OR IGNORE INTO chunks (hash, size_bytes) VALUES (?, ?)`,
			chunk.Hash, chunk.SizeBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert chunk %s: %w", chunk.Hash, err)
		}

		_, err = tx.Exec(
			`INSERT INTO file_chunks (file_id, chunk_hash, chunk_index) VALUES (?, ?, ?)`,
			fileID, chunk.Hash, chunk.Index,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert file_chunk: %w", err)
		}

		for _, nodeID := range chunk.Nodes {
			_, err = tx.Exec(
				`INSERT OR IGNORE INTO chunk_locations (chunk_hash, node_id) VALUES (?, ?)`,
				chunk.Hash, nodeID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to insert chunk location: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &UploadResponse{
		Filename: req.Filename,
		Version:  version,
	}, nil
}

// GetLatestFile returns the most recent version of a file by filename.
func (s *Store) GetLatestFile(filename string) (*FileMetadata, error) {
	return s.getLatestFile(filename)
}

// getLatestFile is the internal implementation.
func (s *Store) getLatestFile(filename string) (*FileMetadata, error) {
	row := s.db.QueryRow(
		`SELECT id, filename, version, size_bytes, file_hash, created_at, updated_at
		 FROM files
		 WHERE filename = ?
		 ORDER BY version DESC
		 LIMIT 1`,
		filename,
	)

	var f FileMetadata
	var createdAt, updatedAt string
	err := row.Scan(
		&f.ID, &f.Filename, &f.Version,
		&f.SizeBytes, &f.FileHash,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("file %q not found", filename)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query file: %w", err)
	}

	f.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	f.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	f.Chunks, err = s.getChunksForFile(f.ID)
	if err != nil {
		return nil, err
	}

	return &f, nil
}

// getChunksForFile loads the ordered chunk list for a file ID.
// We close the rows cursor explicitly before querying node locations,
// because SQLite with one connection cannot have two open cursors at once.
func (s *Store) getChunksForFile(fileID int64) ([]ChunkRef, error) {
	rows, err := s.db.Query(
		`SELECT fc.chunk_index, fc.chunk_hash, c.size_bytes
		 FROM file_chunks fc
		 JOIN chunks c ON fc.chunk_hash = c.hash
		 WHERE fc.file_id = ?
		 ORDER BY fc.chunk_index ASC`,
		fileID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []ChunkRef
	for rows.Next() {
		var chunk ChunkRef
		err := rows.Scan(&chunk.Index, &chunk.Hash, &chunk.SizeBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk row: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	// close explicitly before opening new queries below
	rows.Close()

	// load node locations for each chunk
	// safe to query now because the rows cursor above is closed
	for i, chunk := range chunks {
		nodeRows, err := s.db.Query(
			`SELECT node_id FROM chunk_locations WHERE chunk_hash = ?`,
			chunk.Hash,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to query nodes for chunk %s: %w", chunk.Hash, err)
		}

		var nodes []string
		for nodeRows.Next() {
			var nodeID string
			if err := nodeRows.Scan(&nodeID); err != nil {
				nodeRows.Close()
				return nil, fmt.Errorf("failed to scan node: %w", err)
			}
			nodes = append(nodes, nodeID)
		}
		nodeRows.Close()

		chunks[i].Nodes = nodes
	}

	return chunks, nil
}

// ListFiles returns the latest version of every file in the database.
func (s *Store) ListFiles() ([]FileListItem, error) {
	rows, err := s.db.Query(
		`SELECT f.filename, f.version, f.size_bytes, COUNT(fc.chunk_hash) as chunk_count
		 FROM files f
		 JOIN file_chunks fc ON f.id = fc.file_id
		 WHERE f.version = (
			SELECT MAX(version) FROM files f2 WHERE f2.filename = f.filename
		 )
		 GROUP BY f.id
		 ORDER BY f.filename ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	var files []FileListItem
	for rows.Next() {
		var item FileListItem
		err := rows.Scan(&item.Filename, &item.Version, &item.SizeBytes, &item.ChunkCount)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}
		files = append(files, item)
	}

	return files, nil
}