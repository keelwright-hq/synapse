package memory

import (
	"testing"

	"github.com/taricsa/synapse/internal/graph"
	"github.com/taricsa/synapse/internal/graph/storetest"
)

func TestStoreConformance(t *testing.T) {
	storetest.RunConformance(t, func() graph.Store {
		return New()
	})
}
