// Package uri implements the Synapse repo:// identifier grammar (SYN-11).
package uri

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const (
	Scheme = "repo"

	// PropKey is the node property holding the canonical repo:// URI.
	PropKey = "repo_uri"
)

// Kind tokens as they appear in the URI fragment.
const (
	KindFile     = "file"
	KindPackage  = "package"
	KindModule   = "module"
	KindFunc     = "func"
	KindMethod   = "method"
	KindType     = "type"
	KindImport   = "import"
	KindSymbol   = "symbol"
)

var (
	ErrInvalid   = errors.New("uri: invalid repo:// URI")
	ErrConflict  = errors.New("uri: conflict")
	repoNameRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	validKinds   = map[string]struct{}{
		KindFile: {}, KindPackage: {}, KindModule: {}, KindFunc: {},
		KindMethod: {}, KindType: {}, KindImport: {}, KindSymbol: {},
	}
)

// URI is a parsed, normalized repo:// identifier.
type URI struct {
	Repo   string
	Path   string // slash-separated, no leading slash
	Kind   string // URI kind token (func, method, file, …)
	Symbol string // empty for KindFile
}

// KindToken maps a graph Node.Kind string to the URI fragment token.
func KindToken(nodeKind string) (string, error) {
	switch nodeKind {
	case "file":
		return KindFile, nil
	case "package":
		return KindPackage, nil
	case "module":
		return KindModule, nil
	case "function":
		return KindFunc, nil
	case "method":
		return KindMethod, nil
	case "type":
		return KindType, nil
	case "import":
		return KindImport, nil
	case "symbol":
		return KindSymbol, nil
	default:
		return "", fmt.Errorf("%w: unknown node kind %q", ErrInvalid, nodeKind)
	}
}

// NodeKind maps a URI kind token back to Node.Kind.
func NodeKind(kindToken string) (string, error) {
	switch kindToken {
	case KindFile:
		return "file", nil
	case KindPackage:
		return "package", nil
	case KindModule:
		return "module", nil
	case KindFunc:
		return "function", nil
	case KindMethod:
		return "method", nil
	case KindType:
		return "type", nil
	case KindImport:
		return "import", nil
	case KindSymbol:
		return "symbol", nil
	default:
		return "", fmt.Errorf("%w: unknown kind token %q", ErrInvalid, kindToken)
	}
}

// NormalizeRepo validates and lowercases? — keep case as given but trim.
// Repo names are case-sensitive as provided; only charset is restricted.
func NormalizeRepo(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", fmt.Errorf("%w: empty repo", ErrInvalid)
	}
	if strings.ContainsAny(repo, "/?#") || !repoNameRe.MatchString(repo) {
		return "", fmt.Errorf("%w: repo %q is not URL-safe", ErrInvalid, repo)
	}
	return repo, nil
}

// NormalizePath makes a repo-relative slash path (no leading/trailing slash except empty).
func NormalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") || p == ".." {
		return "", fmt.Errorf("%w: path %q escapes repo root", ErrInvalid, p)
	}
	return p, nil
}

// Build constructs a canonical repo:// URI string.
func Build(repo, filePath, kindToken, symbol string) (string, error) {
	u, err := New(repo, filePath, kindToken, symbol)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// New validates and returns a normalized URI.
func New(repo, filePath, kindToken, symbol string) (URI, error) {
	repo, err := NormalizeRepo(repo)
	if err != nil {
		return URI{}, err
	}
	filePath, err = NormalizePath(filePath)
	if err != nil {
		return URI{}, err
	}
	kindToken = strings.TrimSpace(kindToken)
	if _, ok := validKinds[kindToken]; !ok {
		return URI{}, fmt.Errorf("%w: unknown kind %q", ErrInvalid, kindToken)
	}
	symbol = strings.TrimSpace(symbol)
	if kindToken == KindFile {
		if symbol != "" {
			return URI{}, fmt.Errorf("%w: file kind must not have a symbol", ErrInvalid)
		}
	} else if symbol == "" {
		return URI{}, fmt.Errorf("%w: kind %q requires a symbol", ErrInvalid, kindToken)
	}
	return URI{Repo: repo, Path: filePath, Kind: kindToken, Symbol: symbol}, nil
}

// String returns the canonical encoded form.
func (u URI) String() string {
	pathEsc := escapePath(u.Path)
	frag := u.Kind
	if u.Kind != KindFile {
		frag = u.Kind + ":" + escapeFragmentPart(u.Symbol)
	}
	return Scheme + "://" + u.Repo + "/" + pathEsc + "#" + frag
}

// Parse parses and normalizes a repo:// URI.
func Parse(raw string) (URI, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return URI{}, fmt.Errorf("%w: empty", ErrInvalid)
	}
	if strings.Contains(raw, "?") {
		return URI{}, fmt.Errorf("%w: query parameters are not allowed", ErrInvalid)
	}
	if !strings.HasPrefix(raw, Scheme+"://") {
		return URI{}, fmt.Errorf("%w: must start with %s://", ErrInvalid, Scheme)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if parsed.Scheme != Scheme {
		return URI{}, fmt.Errorf("%w: scheme %q", ErrInvalid, parsed.Scheme)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return URI{}, fmt.Errorf("%w: query parameters are not allowed", ErrInvalid)
	}
	repo := parsed.Host
	if repo == "" {
		// repo://name/path — some parsers put name in Opaque
		return URI{}, fmt.Errorf("%w: missing repo", ErrInvalid)
	}
	repo, err = NormalizeRepo(repo)
	if err != nil {
		return URI{}, err
	}

	p := parsed.EscapedPath()
	if p == "" {
		p = parsed.Path
	}
	p, err = url.PathUnescape(p)
	if err != nil {
		return URI{}, fmt.Errorf("%w: path unescape: %v", ErrInvalid, err)
	}
	p, err = NormalizePath(p)
	if err != nil {
		return URI{}, err
	}

	// url.Parse already decodes Fragment; do not unescape again.
	frag := strings.TrimSpace(parsed.Fragment)
	if frag == "" {
		return URI{}, fmt.Errorf("%w: missing fragment", ErrInvalid)
	}

	kindToken, symbol, err := splitFragment(frag)
	if err != nil {
		return URI{}, err
	}
	return New(repo, p, kindToken, symbol)
}

// Validate reports whether raw is a valid repo:// URI.
func Validate(raw string) error {
	_, err := Parse(raw)
	return err
}

// Normalize returns the canonical string form of raw.
func Normalize(raw string) (string, error) {
	u, err := Parse(raw)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func splitFragment(frag string) (kind, symbol string, err error) {
	if frag == KindFile {
		return KindFile, "", nil
	}
	kind, symbol, ok := strings.Cut(frag, ":")
	if !ok || kind == "" {
		return "", "", fmt.Errorf("%w: fragment must be %q or kind:symbol", ErrInvalid, KindFile)
	}
	if kind == KindFile {
		return "", "", fmt.Errorf("%w: file kind must not have a symbol", ErrInvalid)
	}
	if symbol == "" {
		return "", "", fmt.Errorf("%w: empty symbol in fragment", ErrInvalid)
	}
	return kind, symbol, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func escapeFragmentPart(s string) string {
	// Encode characters that would break fragment parsing or URI structure.
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '%' || r == '#' || r == '?' || r == ' ' || r < 0x20 || r == 0x7f:
			b.WriteString(url.PathEscape(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
