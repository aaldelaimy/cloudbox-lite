package hashring

import (
	"fmt"
	"testing"
)

func newTestRing() *Ring {
	r := NewRing(100)
	r.AddNode("node-1", "localhost:9001")
	r.AddNode("node-2", "localhost:9002")
	r.AddNode("node-3", "localhost:9003")
	return r
}

func TestGetNodeDeterministic(t *testing.T) {
	r := newTestRing()

	// same key must always return the same node
	id1, _, err := r.GetNode("chunk-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	id2, _, err := r.GetNode("chunk-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id1 != id2 {
		t.Errorf("expected same node for same key, got %q and %q", id1, id2)
	}
}

func TestGetNodeDistributes(t *testing.T) {
	r := newTestRing()

	// with enough keys, all three nodes should get at least one chunk
	counts := make(map[string]int)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chunk-%d", i)
		id, _, err := r.GetNode(key)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		counts[id]++
	}

	for _, nodeID := range []string{"node-1", "node-2", "node-3"} {
		if counts[nodeID] == 0 {
			t.Errorf("node %s received no chunks - distribution is uneven", nodeID)
		}
	}
}

func TestGetNodesReplication(t *testing.T) {
	r := newTestRing()

	nodes, err := r.GetNodes("chunk-abc", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// the two nodes must be different
	if nodes[0].ID == nodes[1].ID {
		t.Errorf("expected different nodes for replication, got same: %q", nodes[0].ID)
	}
}

func TestGetNodesAllDistinct(t *testing.T) {
	r := newTestRing()

	// ask for 3 nodes - should get all 3 distinct nodes
	nodes, err := r.GetNodes("chunk-xyz", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	seen := make(map[string]bool)
	for _, n := range nodes {
		if seen[n.ID] {
			t.Errorf("duplicate node in result: %q", n.ID)
		}
		seen[n.ID] = true
	}
}

func TestGetNodesCapAtAvailable(t *testing.T) {
	r := newTestRing()

	// ask for more nodes than exist - should return only what's available
	nodes, err := r.GetNodes("chunk-abc", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("expected 3 nodes (capped at available), got %d", len(nodes))
	}
}

func TestEmptyRing(t *testing.T) {
	r := NewRing(100)

	_, _, err := r.GetNode("anything")
	if err == nil {
		t.Error("expected error for empty ring, got nil")
	}
}

func TestRemoveNode(t *testing.T) {
	r := newTestRing()

	r.RemoveNode("node-2")

	if r.Len() != 2 {
		t.Errorf("expected 2 nodes after removal, got %d", r.Len())
	}

	// after removal, node-2 should never be returned
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chunk-%d", i)
		id, _, err := r.GetNode(key)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id == "node-2" {
			t.Errorf("removed node-2 was still returned for key %q", key)
		}
	}
}

func TestRemappingOnNodeRemoval(t *testing.T) {
	r := newTestRing()

	// record where 100 chunks land before removal
	before := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chunk-%d", i)
		id, _, _ := r.GetNode(key)
		before[key] = id
	}

	r.RemoveNode("node-2")

	// count how many chunks moved after removal
	moved := 0
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chunk-%d", i)
		id, _, _ := r.GetNode(key)
		if id != before[key] && before[key] != "node-2" {
			moved++
		}
	}

	// chunks that were NOT on node-2 should not have moved
	if moved > 0 {
		t.Errorf("consistent hashing violated: %d chunks moved that were not on node-2", moved)
	}
}