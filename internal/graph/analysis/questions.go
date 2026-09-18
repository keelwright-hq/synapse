package analysis

import (
	"fmt"

	"github.com/keelwright-hq/synapse/internal/graph"
)

// Question represents an auto-generated architectural navigation or refactoring question.
type Question struct {
	Text      string `json:"text"`
	Rationale string `json:"rationale"`
}

// GenerateQuestions programmatically creates navigation questions based on topological metrics.
// coverage qualifies advice when import/call resolution is incomplete.
func GenerateQuestions(nodes []graph.Node, communities []Community, centralities []NodeCentrality, cycles []Cycle, coverage Coverage) []Question {
	var questions []Question
	_ = nodes

	// 1. High betweenness centrality bridge question
	if len(centralities) > 0 {
		top := centralities[0]
		if top.Betweenness > 0 {
			questions = append(questions, Question{
				Text:      fmt.Sprintf("Why does `%s` act as a central bridge across different components?", top.Name),
				Rationale: fmt.Sprintf("High betweenness centrality score (%.2f) - changes to this node impact multiple execution paths.", top.Betweenness),
			})
		}
	}

	// 2. Low cohesion community refactoring question — skip unqualified sprawl
	// advice when no imports could be resolved.
	importsUnresolved := coverage.ImportEdgesResolved == 0 && coverage.ImportEdgesTotal > 0
	for _, c := range communities {
		if len(c.NodeIDs) > 10 && c.Cohesion < 0.05 {
			if importsUnresolved {
				questions = append(questions, Question{
					Text: fmt.Sprintf("Is community `%s` a real module boundary, or an artifact of incomplete dependency resolution?", c.Name),
					Rationale: fmt.Sprintf(
						"Low cohesion (%.2f) across %d nodes, but 0/%d imports were resolved — do not treat this as unqualified module sprawl.",
						c.Cohesion, len(c.NodeIDs), coverage.ImportEdgesTotal),
				})
			} else {
				questions = append(questions, Question{
					Text:      fmt.Sprintf("Should community `%s` be split into smaller, more focused modules?", c.Name),
					Rationale: fmt.Sprintf("Low cohesion score (%.2f) across %d nodes - indicates module sprawl.", c.Cohesion, len(c.NodeIDs)),
				})
			}
			break
		}
	}

	// 3. Cycle refactoring question — or disclose incomplete coverage when empty.
	if len(cycles) > 0 {
		cyc := cycles[0]
		questions = append(questions, Question{
			Text:      fmt.Sprintf("How can the dependency cycle of length %d involving `%s` be decoupled?", cyc.Length, cyc.Path[0]),
			Rationale: "Circular dependency loops hinder modularity and increase build coupling.",
		})
	} else if coverage.Incomplete() && coverage.ImportEdgesTotal > 0 {
		questions = append(questions, Question{
			Text: "Which unresolved imports should be supported next to improve cycle detection coverage?",
			Rationale: fmt.Sprintf(
				"No cycles found within resolved edges (%d/%d imports resolved); this is not a repo-wide claim that none exist.",
				coverage.ImportEdgesResolved, coverage.ImportEdgesTotal),
		})
	}

	return questions
}
