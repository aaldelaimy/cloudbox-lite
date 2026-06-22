package blockstore

import (
	"errors"
	"os"
	"testing"
)

// newTestStore creates a Store in a temporary directory.
// The directory is automatically deleted after the test finishes.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store
}

func TestPutAndGet(t *testing.T) {
	store := newTestStore(t)

	data := []byte("hello cloudbox")
	hash := "testhash123"

	_, err := store.Put(hash, data)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := store.Get(hash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestHas(t *testing.T) {
	store := newTestStore(t)

	if store.Has("nonexistent") {
		t.Error("expected Has to return false for nonexistent chunk")
	}

	store.Put("myhash", []byte("some data"))

	if !store.Has("myhash") {
		t.Error("expected Has to return true after Put")
	}
}

func TestDuplicate(t *testing.T) {
	store := newTestStore(t)

	data := []byte("duplicate me")
	hash := "dupehash"

	dup1, err := store.Put(hash, data)
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}
	if dup1 {
		t.Error("expected first Put to not be a duplicate")
	}

	dup2, err := store.Put(hash, data)
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}
	if !dup2 {
		t.Error("expected second Put to be detected as duplicate")
	}
}

func TestGetNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Get("doesnotexist")
	if err == nil {
		t.Fatal("expected error for missing chunk, got nil")
	}
	if !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("expected ErrChunkNotFound, got %v", err)
	}
}

func TestStoreCreatesDirectory(t *testing.T) {
	dir := t.TempDir() + "/newsubdir"

	_, err := NewStore(dir)
	if err != nil {
		t.Fatalf("expected NewStore to create directory, got error: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}