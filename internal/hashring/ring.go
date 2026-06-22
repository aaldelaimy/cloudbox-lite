package hashring

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// node represents one physical storage node.
type node struct {
	ID      string
	Address string
}

// virtualNode is one point on the hash ring representing a physical node.
type virtualNode struct {
	hash     uint32 // position on the ring
	nodeID   string // which physical node this belongs to
}

// Ring is a consistent hash ring.
type Ring struct {
	mu           sync.RWMutex
	nodes        map[string]node // nodeID -> node
	virtualNodes []virtualNode   // sorted by hash position
	replication  int             // how many virtual nodes per physical node
}

// NewRing creates a new hash ring.
// replication is the number of virtual nodes per physical node.
// Higher values give more even distribution but use more memory.
func NewRing(replication int) *Ring {
	if replication <= 0 {
		replication = 100
	}
	return &Ring{
		nodes:       make(map[string]node),
		replication: replication,
	}
}

// AddNode adds a physical node to the ring.
// It creates `replication` virtual nodes spread around the ring.
func (r *Ring) AddNode(id string, address string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodes[id] = node{ID: id, Address: address}

	for i := 0; i < r.replication; i++ {
		key := fmt.Sprintf("%s-%d", id, i)
		h := hashKey(key)
		r.virtualNodes = append(r.virtualNodes, virtualNode{
			hash:   h,
			nodeID: id,
		})
	}

	// keep virtual nodes sorted by hash position
	sort.Slice(r.virtualNodes, func(i, j int) bool {
		return r.virtualNodes[i].hash < r.virtualNodes[j].hash
	})
}

// RemoveNode removes a physical node and all its virtual nodes from the ring.
func (r *Ring) RemoveNode(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, id)

	// filter out all virtual nodes belonging to this node
	var remaining []virtualNode
	for _, vn := range r.virtualNodes {
		if vn.nodeID != id {
			remaining = append(remaining, vn)
		}
	}
	r.virtualNodes = remaining
}

// GetNode returns the node responsible for the given key.
// Walks clockwise from the key's position to find the first node.
func (r *Ring) GetNode(key string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.virtualNodes) == 0 {
		return "", "", fmt.Errorf("hash ring is empty")
	}

	h := hashKey(key)

	// binary search for the first virtual node at or after position h
	idx := sort.Search(len(r.virtualNodes), func(i int) bool {
		return r.virtualNodes[i].hash >= h
	})

	// if we walked past the end, wrap around to the first node
	if idx == len(r.virtualNodes) {
		idx = 0
	}

	nodeID := r.virtualNodes[idx].nodeID
	n := r.nodes[nodeID]
	return n.ID, n.Address, nil
}

// GetNodes returns up to count distinct nodes for the given key.
// Used for replication - returns the primary node and its replicas.
func (r *Ring) GetNodes(key string, count int) ([]node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.virtualNodes) == 0 {
		return nil, fmt.Errorf("hash ring is empty")
	}

	if count > len(r.nodes) {
		count = len(r.nodes)
	}

	h := hashKey(key)
	idx := sort.Search(len(r.virtualNodes), func(i int) bool {
		return r.virtualNodes[i].hash >= h
	})
	if idx == len(r.virtualNodes) {
		idx = 0
	}

	// walk clockwise collecting distinct physical nodes
	seen := make(map[string]bool)
	var result []node

	for len(result) < count {
		vn := r.virtualNodes[idx%len(r.virtualNodes)]
		if !seen[vn.nodeID] {
			seen[vn.nodeID] = true
			result = append(result, r.nodes[vn.nodeID])
		}
		idx++
		// safety: if we've lapped the ring without finding enough nodes, stop
		if idx > len(r.virtualNodes)+1 {
			break
		}
	}

	return result, nil
}

// Len returns the number of physical nodes in the ring.
func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// hashKey converts a string key into a uint32 ring position.
func hashKey(key string) uint32 {
	h := sha256.Sum256([]byte(key))
	// take the first 4 bytes and interpret them as a uint32
	return binary.BigEndian.Uint32(h[:4])
}