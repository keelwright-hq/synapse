package uri

import (
	"fmt"
	"strings"
)

// LegacyParts holds components derived from a Phase-1 Node.ID.
type LegacyParts struct {
	KindToken string
	Path      string
	Symbol    string
	// GlobalSymbol is true for unresolved targets like "symbol:Name" with no path.
	GlobalSymbol bool
}

// ParseLegacyID parses Phase-1 ids such as "func:path#Name" or "file:path".
func ParseLegacyID(id string) (LegacyParts, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return LegacyParts{}, fmt.Errorf("%w: empty legacy id", ErrInvalid)
	}

	kind, rest, ok := strings.Cut(id, ":")
	if !ok || kind == "" || rest == "" {
		return LegacyParts{}, fmt.Errorf("%w: malformed legacy id %q", ErrInvalid, id)
	}

	switch kind {
	case "file", "module":
		p, err := NormalizePath(rest)
		if err != nil {
			return LegacyParts{}, err
		}
		if kind == "file" {
			return LegacyParts{KindToken: KindFile, Path: p}, nil
		}
		// module:path — fragment needs a symbol; use path basename.
		return LegacyParts{KindToken: KindModule, Path: p, Symbol: pathBase(p)}, nil
	case "symbol":
		// Global unresolved: symbol:Name (no path)
		if !strings.Contains(rest, "#") && !strings.Contains(rest, "/") {
			return LegacyParts{KindToken: KindSymbol, Symbol: rest, GlobalSymbol: true}, nil
		}
		// Unusual: treat as path#symbol if present
		pathPart, sym, cut := strings.Cut(rest, "#")
		if !cut {
			return LegacyParts{KindToken: KindSymbol, Symbol: rest, GlobalSymbol: true}, nil
		}
		p, err := NormalizePath(pathPart)
		if err != nil {
			return LegacyParts{}, err
		}
		return LegacyParts{KindToken: KindSymbol, Path: p, Symbol: sym}, nil
	case "func", "method", "type", "package", "import":
		pathPart, sym, cut := strings.Cut(rest, "#")
		if !cut || pathPart == "" || sym == "" {
			return LegacyParts{}, fmt.Errorf("%w: malformed legacy id %q", ErrInvalid, id)
		}
		p, err := NormalizePath(pathPart)
		if err != nil {
			return LegacyParts{}, err
		}
		return LegacyParts{KindToken: kind, Path: p, Symbol: sym}, nil
	default:
		return LegacyParts{}, fmt.Errorf("%w: unknown legacy kind %q", ErrInvalid, kind)
	}
}

// FromLegacy builds a repo:// URI from a Phase-1 node id and repo name.
// Global symbol ids return ok=false without error (no URI assigned).
func FromLegacy(repo, legacyID string) (canonical string, ok bool, err error) {
	parts, err := ParseLegacyID(legacyID)
	if err != nil {
		return "", false, err
	}
	if parts.GlobalSymbol {
		return "", false, nil
	}
	s, err := Build(repo, parts.Path, parts.KindToken, parts.Symbol)
	if err != nil {
		return "", false, err
	}
	return s, true, nil
}

// Assign builds a URI for a node from repo, path, and Node.Kind / name / props.
// Global unresolved symbols (kind symbol with empty path or id prefix symbol: without path) skip.
func Assign(repo, filePath, nodeKind, name, legacyID string) (canonical string, ok bool, err error) {
	tok, err := KindToken(nodeKind)
	if err != nil {
		return "", false, err
	}
	if tok == KindSymbol {
		// Shared Phase-1 ids (symbol:Name) are global; only assign a URI when the
		// legacy id is already path-scoped (unusual) or we invent file-scoped ids later.
		if parts, perr := ParseLegacyID(legacyID); perr == nil && parts.GlobalSymbol {
			return "", false, nil
		}
		if filePath == "" {
			return "", false, nil
		}
		sym := name
		if sym == "" {
			if parts, perr := ParseLegacyID(legacyID); perr == nil {
				sym = parts.Symbol
			}
		}
		if sym == "" {
			return "", false, nil
		}
		s, err := Build(repo, filePath, KindSymbol, sym)
		if err != nil {
			return "", false, err
		}
		return s, true, nil
	}
	if filePath == "" {
		return "", false, fmt.Errorf("%w: missing path for kind %s", ErrInvalid, tok)
	}
	sym := name
	if tok == KindFile {
		sym = ""
	} else if tok == KindMethod {
		// Prefer legacy id symbol (Recv.Name) over bare method name
		if parts, perr := ParseLegacyID(legacyID); perr == nil && parts.Symbol != "" {
			sym = parts.Symbol
		} else if strings.Contains(legacyID, "#") {
			_, rest, _ := strings.Cut(legacyID, "#")
			if rest != "" {
				sym = rest
			}
		}
	} else if tok == KindImport || tok == KindPackage || tok == KindType || tok == KindFunc || tok == KindModule {
		if parts, perr := ParseLegacyID(legacyID); perr == nil && parts.Symbol != "" {
			sym = parts.Symbol
		}
	}
	if tok == KindModule {
		// module:path — symbol empty in legacy; use basename or "module"?
		// Phase-1 moduleID is "module:"+path with no #symbol.
		// Grammar example doesn't show module; use fragment module:{last path segment} or path?
		// Use name if set, else base of path.
		if sym == "" {
			sym = pathBase(filePath)
		}
	}
	s, err := Build(repo, filePath, tok, sym)
	if err != nil {
		return "", false, err
	}
	return s, true, nil
}

func pathBase(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
