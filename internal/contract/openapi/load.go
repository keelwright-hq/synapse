// Package openapi loads OpenAPI 3.x specs and maps them into Synapse graph IR.
package openapi

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// openAPI3Re matches OpenAPI 3.x version fields in YAML or JSON.
var openAPI3Re = regexp.MustCompile(`(?m)["']?openapi["']?\s*:\s*["']?3\.`)

// LooksLikeOpenAPI reports whether data appears to be an OpenAPI 3.x document.
func LooksLikeOpenAPI(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return openAPI3Re.Match(data)
}

// Load reads and parses an OpenAPI 3.x document from path.
func Load(path string) (*openapi3.T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data)
}

// LoadBytes parses OpenAPI 3.x YAML or JSON from data.
func LoadBytes(data []byte) (*openapi3.T, error) {
	if !LooksLikeOpenAPI(data) {
		return nil, fmt.Errorf("openapi: not an OpenAPI 3.x document")
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("openapi: load: %w", err)
	}
	if doc.OpenAPI == "" || !strings.HasPrefix(doc.OpenAPI, "3.") {
		return nil, fmt.Errorf("openapi: unsupported version %q (want 3.x)", doc.OpenAPI)
	}
	return doc, nil
}
