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
func GenerateQuestions(nodes []graph.Node, communities []Community, centralities []NodeCentrality, cycles []Cycle) []Question {
	var questions []Question

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

	// 2. Low cohesion community refactoring question
	for _, c := range communities {
		if len(c.NodeIDs) > 10 && c.Cohesion < 0.05 {
			questions = append(questions, Question{
				Text:      fmt.Sprintf("Should community `%s` be split into smaller, more focused modules?", c.Name),
				Rationale: fmt.Sprintf("Low cohesion score (%.2f) across %d nodes - indicates module sprawl.", c.Cohesion, len(c.NodeIDs)),
				})
			break
		}
	}

	// 3. Cycle refactoring question
	if len(cycles) > 0 {
		cyc := cycles[0]
		questions = append(questions, Question{
			Text:      fmt.Sprintf("How can the dependency cycle of length %d involving `%s` be decoupled?", cyc.Length, cyc.Path[0]),
			Rationale: "Circular dependency loops hinder modularity and increase build coupling.",
		})
	}

	return questions
}
