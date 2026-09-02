package badger

import (
	"fmt"
	"testing"

	"github.com/taricsa/synapse/internal/graph"
)

const benchNodeCount = 10000

func populateStore(s *Store, n int) error {
	for i := 0; i < n; i++ {
		id := graph.NodeID(fmt.Sprintf("node-%d", i))
		if err := s.PutNode(graph.Node{ID: id, Kind: "symbol", Name: string(id)}); err != nil {
			return err
		}
		if i > 0 {
			prev := graph.NodeID(fmt.Sprintf("node-%d", i-1))
			if err := s.PutEdge(graph.Edge{From: prev, To: id, Type: "next"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func BenchmarkPutNode(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := graph.NodeID(fmt.Sprintf("bench-%d", i))
		if err := s.PutNode(graph.Node{ID: id, Kind: "symbol"}); err != nil {
			b.Fatalf("PutNode: %v", err)
		}
	}
}

func BenchmarkGetNode(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := populateStore(s, benchNodeCount); err != nil {
		b.Fatalf("populate: %v", err)
	}

	target := graph.NodeID("node-5000")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetNode(target); err != nil {
			b.Fatalf("GetNode: %v", err)
		}
	}
}

func BenchmarkOutEdges(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if err := populateStore(s, benchNodeCount); err != nil {
		b.Fatalf("populate: %v", err)
	}

	from := graph.NodeID("node-9999")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.OutEdges(from, ""); err != nil {
			b.Fatalf("OutEdges: %v", err)
		}
	}
}

// BenchmarkPopulate10k documents write throughput for ≥10k nodes (SYN-5 AC).
// Run: go test -bench=BenchmarkPopulate10k -benchmem ./internal/store/badger/
func BenchmarkPopulate10k(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := populateStore(s, benchNodeCount); err != nil {
			b.Fatalf("populate: %v", err)
		}
	}
}
