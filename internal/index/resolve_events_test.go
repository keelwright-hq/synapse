package index_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/index"
	"github.com/keelwright-hq/synapse/internal/parse"
	"github.com/keelwright-hq/synapse/internal/rank"
	"github.com/keelwright-hq/synapse/internal/store/memory"
)

func TestIndexerLinksInngestEventsToFunctions(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"src/inngest", "src/lib", "src/app/api/publish", "src/app/api/approve"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGo(t, filepath.Join(root, "src/lib/auth.ts"), `export async function getUser() { return null; }
`)
	writeGo(t, filepath.Join(root, "src/inngest/functions.ts"), `import { inngest } from "./client";

export const publishPostFn = inngest.createFunction(
  { id: "publish-post", triggers: [{ event: "post/publish" }] },
  async ({ event, step }) => {
    await step.sendEvent("requeue", { name: "post/publish", data: event.data });
  }
);
`)
	writeGo(t, filepath.Join(root, "src/lib/approve.ts"), `import { insertOutboxEvent } from "./outbox";

export async function decideApproval(postId: string) {
  await db.transaction(async (tx) => {
    await insertOutboxEvent({ eventName: "post/publish", payload: { postId }, tx });
  });
}
`)
	writeGo(t, filepath.Join(root, "src/app/api/approve/route.ts"), `import { getUser } from "@/lib/auth";
import { decideApproval } from "@/lib/approve";

export async function POST(req: Request) {
  const user = await getUser();
  await decideApproval("p1");
  return Response.json({ ok: true });
}
`)
	publishRoute := filepath.Join(root, "src/app/api/publish/route.ts")
	writeGo(t, publishRoute, `import { inngest } from "@/inngest/client";

export async function POST(req: Request) {
  await inngest.send({ name: "post/publish", data: {} });
  return Response.json({ ok: true });
}
`)

	store := memory.New()
	idx := index.New(store)
	if _, err := idx.Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	const fn = graph.NodeID("func:src/inngest/functions.ts#publishPostFn")
	edgeTo := func(from graph.NodeID) *graph.Edge {
		t.Helper()
		es, err := store.OutEdges(from, parse.EdgeTriggers)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			if e.To == fn {
				return &e
			}
		}
		return nil
	}
	e := edgeTo("func:src/app/api/publish/route.ts#POST")
	if e == nil {
		t.Fatal("want triggers edge from publish route POST to publishPostFn")
	}
	if e.Props[index.PropEventName] != "post/publish" || e.Props[index.PropEventVia] != "send" {
		t.Fatalf("edge props = %v", e.Props)
	}
	if e := edgeTo("func:src/lib/approve.ts#decideApproval"); e == nil || e.Props[index.PropEventVia] != "outbox" {
		t.Fatalf("want outbox triggers edge from decideApproval, got %v", e)
	}
	// A function that requeues its own event does not trigger itself.
	if e := edgeTo(fn); e != nil {
		t.Fatalf("unexpected self triggers edge: %v", e)
	}

	// A route handler reaches the function through a helper that writes the outbox row.
	p, err := rank.ShortestPath(store, "func:src/app/api/approve/route.ts#POST", fn, 0)
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, st := range p.Steps {
		steps = append(steps, st.Edge+":"+st.Name)
	}
	if got := strings.Join(steps, " "); got != ":POST calls:decideApproval triggers:publishPostFn" {
		t.Fatalf("path = %s", got)
	}
	aff, err := rank.Affected(store, fn, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := impactNames(aff); got != "POST,decideApproval" {
		t.Fatalf("affected by publishPostFn = %q", got)
	}

	// Re-index after the route stops sending: its edge goes away.
	writeGo(t, publishRoute, `export async function POST(req: Request) {
  return Response.json({ ok: true });
}
`)
	if _, err := idx.Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	if e := edgeTo("func:src/app/api/publish/route.ts#POST"); e != nil {
		t.Fatalf("stale triggers edge after the send was removed: %v", e)
	}
	// Re-indexing the receiver's file keeps the edge from an unchanged sender.
	writeGo(t, filepath.Join(root, "src/inngest/functions.ts"), `import { inngest } from "./client";

// edited
export const publishPostFn = inngest.createFunction(
  { id: "publish-post", triggers: [{ event: "post/publish" }] },
  async () => {}
);
`)
	if _, err := idx.Run(root, index.Options{Workers: 2}); err != nil {
		t.Fatal(err)
	}
	if e := edgeTo("func:src/lib/approve.ts#decideApproval"); e == nil {
		t.Fatal("triggers edge lost after the receiver's file was re-indexed")
	}
}
