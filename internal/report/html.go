package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed graph.html.tmpl
var graphHTMLTemplate string

type htmlMeta struct {
	Repo        string         `json:"repo"`
	Root        string         `json:"root"`
	Commit      string         `json:"commit,omitempty"`
	Timestamp   string         `json:"timestamp"`
	NodeCount   int            `json:"node_count"`
	EdgeCount   int            `json:"edge_count"`
	LanguageMix map[string]int `json:"language_mix"`
	Languages   []string       `json:"languages"`
}

func renderHTML(man manifest, gdoc graphDoc) ([]byte, error) {
	meta := htmlMeta{
		Repo:        man.Repo,
		Root:        man.Root,
		Commit:      man.Commit,
		Timestamp:   man.Timestamp,
		NodeCount:   man.NodeCount,
		EdgeCount:   man.EdgeCount,
		LanguageMix: man.LanguageMix,
		Languages:   man.Languages,
	}
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("report: encode html meta: %w", err)
	}
	graphRaw, err := json.Marshal(gdoc)
	if err != nil {
		return nil, fmt.Errorf("report: encode html graph: %w", err)
	}
	// Prevent </script> breakouts when embedding JSON in a script tag.
	metaSafe := strings.ReplaceAll(string(metaRaw), "</", "<\\/")
	graphSafe := strings.ReplaceAll(string(graphRaw), "</", "<\\/")

	out := graphHTMLTemplate
	out = strings.Replace(out, "__SYNAPSE_META_JSON__", metaSafe, 1)
	out = strings.Replace(out, "__SYNAPSE_GRAPH_JSON__", graphSafe, 1)
	if strings.Contains(out, "__SYNAPSE_META_JSON__") || strings.Contains(out, "__SYNAPSE_GRAPH_JSON__") {
		return nil, fmt.Errorf("report: html template placeholders not replaced")
	}
	return []byte(out), nil
}
