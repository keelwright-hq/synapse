package badger_test

import (
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/store/badger"
)

func TestFingerprintRoundTrip(t *testing.T) {
	s, err := badger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, ok, err := s.GetFingerprint("a.go"); err != nil || ok {
		t.Fatalf("expected missing fingerprint, ok=%v err=%v", ok, err)
	}
	if err := s.PutFingerprint("a.go", "abc"); err != nil {
		t.Fatal(err)
	}
	hash, ok, err := s.GetFingerprint("a.go")
	if err != nil || !ok || hash != "abc" {
		t.Fatalf("got hash=%q ok=%v err=%v", hash, ok, err)
	}
	all, err := s.ListFingerprints()
	if err != nil {
		t.Fatal(err)
	}
	if all["a.go"] != "abc" {
		t.Fatalf("list: %v", all)
	}
	if err := s.DeleteFingerprint("a.go"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetFingerprint("a.go"); err != nil || ok {
		t.Fatalf("expected deleted, ok=%v err=%v", ok, err)
	}
}

func TestOwnershipRoundTrip(t *testing.T) {
	s, err := badger.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ids := []graph.NodeID{"file:a.go", "func:a.go#Main"}
	if err := s.PutOwnedNodes("a.go", ids); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOwnedNodes("a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("got %v", got)
	}
	if err := s.DeleteOwnedNodes("a.go"); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetOwnedNodes("a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}
