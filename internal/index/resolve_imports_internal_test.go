package index

import "testing"

func TestResolveImportPath(t *testing.T) {
	files := map[string]bool{
		"web/lib/util.js":         true, // both util.js and util.ts exist
		"web/lib/util.ts":         true,
		"web/lib/http.ts":         true,
		"web/lib/esm.mts":         true,
		"web/lib/cjs.cts":         true,
		"web/models/index.ts":     true,
		"web/app.tsx":             true,
		"pkg/app/__init__.py":     true,
		"pkg/app/models/user.py":  true,
		"pkg/app/util.py":         true,
		"svc/src/core/config.py":  true,
		"svc/src/core/handler.py": true,
	}
	for _, c := range []struct {
		importer, spec, want string
	}{
		// A real .js file wins over the .ts it might have been emitted from.
		{"web/app.tsx", "./lib/util.js", "web/lib/util.js"},
		{"web/app.tsx", "./lib/http.js", "web/lib/http.ts"},
		{"web/app.tsx", "./lib/http", "web/lib/http.ts"},
		{"web/app.tsx", "./lib/esm.mjs", "web/lib/esm.mts"},
		{"web/app.tsx", "./lib/cjs.cjs", "web/lib/cjs.cts"},
		{"web/lib/http.ts", "../models", "web/models/index.ts"},
		// Packages and path aliases are not local files.
		{"web/app.tsx", "react", ""},
		{"web/app.tsx", "@/lib/http", ""},
		{"web/app.tsx", "./lib/missing", ""},
		// Python: relative (one and two dots) and absolute from a nested root.
		{"pkg/app/models/user.py", "..util", "pkg/app/util.py"},
		{"pkg/app/models/user.py", "..", "pkg/app/__init__.py"},
		{"pkg/app/util.py", ".models.user", "pkg/app/models/user.py"},
		{"svc/src/core/handler.py", "core.config", "svc/src/core/config.py"},
		{"svc/src/core/handler.py", "requests", ""},
	} {
		got, ok := resolveImportPath(c.importer, c.spec, files)
		if !ok {
			got = ""
		}
		if got != c.want {
			t.Errorf("resolveImportPath(%q, %q) = %q, want %q", c.importer, c.spec, got, c.want)
		}
	}
}
