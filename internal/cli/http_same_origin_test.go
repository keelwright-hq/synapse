package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Next.js app's client components call its own app/api routes with
// root-relative URLs. Single-repo indexing links them (match=same_origin), so
// `affected` on a route handler reaches the components that call it.
func TestSingleRepoNextSameOriginLinking(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/app/api/users/route.ts": `export async function GET() { return Response.json([]); }
export async function POST(req: Request) { return Response.json({}); }
`,
		"src/app/api/users/[id]/route.ts": `export async function GET() { return Response.json({}); }
`,
		"src/components/user-list.tsx": `'use client';
export async function loadUsers() {
  const res = await fetch('/api/users');
  return res.json();
}
export async function loadUser(id: string) {
  return fetch(` + "`/api/users/${encodeURIComponent(id)}`" + `);
}
export async function addUser(name: string) {
  return fetch('/api/users', { method: 'POST', body: JSON.stringify({ name }) });
}
export async function profile() {
  return fetch('https://api.example.com/api/users');
}
`,
	}
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data := t.TempDir()
	runCLI(t, "index", root, "--data-dir", data, "--repo", "next")

	affected := func(uri string) string {
		t.Helper()
		return string(runCLI(t, "query", "affected", uri, "--data-dir", data, "--repo", "next"))
	}
	list := affected("repo://next/src/app/api/users/route.ts#func:GET")
	if !strings.Contains(list, "src/components/user-list.tsx: loadUsers") {
		t.Errorf("GET /api/users affected:\n%s", list)
	}
	// The absolute URL to another host is not a call to this app.
	if strings.Contains(list, "profile") {
		t.Errorf("absolute URL linked to own route:\n%s", list)
	}
	if post := affected("repo://next/src/app/api/users/route.ts#func:POST"); !strings.Contains(post, "addUser") || strings.Contains(post, "loadUsers") {
		t.Errorf("POST /api/users affected:\n%s", post)
	}
	if one := affected("repo://next/src/app/api/users/%5Bid%5D/route.ts#func:GET"); !strings.Contains(one, "loadUser") {
		t.Errorf("GET /api/users/{} affected:\n%s", one)
	}
}
