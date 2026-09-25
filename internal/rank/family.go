package rank

import (
	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// EdgeFamily groups edge types for fused ranking (SYN-18 / SYN-86).
type EdgeFamily string

const (
	FamilyStatic  EdgeFamily = "static"
	FamilyGit     EdgeFamily = "git"
	FamilyRuntime EdgeFamily = "runtime"
	FamilyDoc     EdgeFamily = "doc"
)

// FamilyOf returns the semantic family for an edge type.
func FamilyOf(t graph.EdgeType) EdgeFamily {
	switch t {
	case parse.EdgeCoCommitted:
		return FamilyGit
	case parse.EdgeObservedCalls:
		return FamilyRuntime
	case parse.EdgeDocuments:
		return FamilyDoc
	default:
		return FamilyStatic
	}
}

// DefaultFamilyWeights multiply per-type DefaultEdgeWeights when building a fused map.
// Values of 1.0 leave defaults unchanged.
var DefaultFamilyWeights = map[EdgeFamily]float64{
	FamilyStatic:  1.0,
	FamilyGit:     1.0,
	FamilyRuntime: 1.0,
	FamilyDoc:     1.0,
}

// WeightsFromFamilies builds a per-type weight map from family multipliers.
// Missing families default to 1.0. Base type weights come from DefaultEdgeWeights.
func WeightsFromFamilies(families map[EdgeFamily]float64) map[graph.EdgeType]float64 {
	out := make(map[graph.EdgeType]float64, len(DefaultEdgeWeights))
	for t, w := range DefaultEdgeWeights {
		mult := 1.0
		if families != nil {
			if m, ok := families[FamilyOf(t)]; ok {
				mult = m
			}
		}
		out[t] = w * mult
	}
	return out
}
