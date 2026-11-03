package index

import "testing"

func TestNextRoutePath(t *testing.T) {
	for _, c := range []struct {
		file, want string
		ok         bool
	}{
		{"src/app/api/users/[id]/route.ts", "/api/users/{}", true},
		{"app/(marketing)/api/apply/route.js", "/api/apply", true},
		{"app/@modal/api/x/route.ts", "/api/x", true},
		{"pages/api/whitepaper.ts", "/api/whitepaper", true},
		{"pages/api/users/index.ts", "/api/users", true},
		{"src/app/api/users/page.tsx", "", false},
		{"src/lib/route.ts", "", false},
	} {
		got, ok := nextRoutePath(c.file)
		if got != c.want || ok != c.ok {
			t.Errorf("nextRoutePath(%q) = %q, %v; want %q, %v", c.file, got, ok, c.want, c.ok)
		}
	}
}
