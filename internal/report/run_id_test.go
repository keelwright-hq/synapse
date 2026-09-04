package report

import (
	"strings"
	"testing"
	"time"
)

func TestNewRunIDUniqueSameInstant(t *testing.T) {
	now := time.Date(2026, 9, 3, 22, 50, 54, 0, time.UTC)
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id := newRunID(now)
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate run id %q", id)
		}
		seen[id] = struct{}{}
		if !strings.HasPrefix(id, "20260903T225054.000Z-") {
			t.Fatalf("unexpected id format %q", id)
		}
	}
}
