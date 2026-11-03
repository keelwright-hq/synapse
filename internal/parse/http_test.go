package parse_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

func TestNormalizeHTTPPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/v1/users/:id?x=1":  "/api/v1/users/{}",
		"api//v1/users/{id}/":    "/api/v1/users/{}",
		"/items/[slug]#top":      "/items/{}",
		"/u/<int:user_id>/posts": "/u/{}/posts",
		"/a/{}-{}":               "/a/{}",
		"":                       "/",
	} {
		if got := parse.NormalizeHTTPPath(in); got != want {
			t.Errorf("NormalizeHTTPPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTTPRecords(t *testing.T) {
	root := t.TempDir()
	type want struct {
		node, prop, value string
	}
	cases := []struct {
		file, src string
		props     []want
	}{
		{
			file: "app.js",
			src: `const express = require('express');
const users = require('./routes/users');
const { adminRouter } = require('./routes/admin');
const app = express();
function mount(suffix, ...handlers) {
  app.use(` + "`/v1${suffix}`" + `, ...handlers);
}
mount('/users', auth, users);
app.use('/teams', require('./routes/teams'));
app.get('/health', (req, res) => res.send('ok'));
app.use('/admin', adminRouter);
module.exports = app;
`,
			props: []want{
				{"module:app.js", parse.PropHTTPRouters, "app||1|app"},
				// A router imported by name mounts that export, not the module's default.
				{"module:app.js", parse.PropHTTPMounts, "/v1/users|app||./routes/users,/teams|app||./routes/teams,/admin|app|adminRouter|./routes/admin"},
				{"module:app.js", parse.PropHTTPRoutes, "GET|/health|app||10"},
			},
		},
		{
			file: "routes.js",
			src: `const router = require('express').Router();
const BASE = process.env.PERF_URL;
router.get('/', listItems);
router.route('/:id').get(showItem);
router.post('/:id/sync', async (req, res) => {
  const url = ` + "`${BASE.replace(/\\/$/, '')}/api/v1/items/${req.params.id}?full=1`" + `;
  await axios.post(url, {});
});
module.exports = router;
`,
			props: []want{
				{"module:routes.js", parse.PropHTTPRoutes, "GET|/|router|listItems|3,GET|/:id|router|showItem|4,POST|/:id/sync|router||5"},
				// The call is in an anonymous handler: it belongs to the module.
				{"module:routes.js", parse.PropHTTPCalls, "direct|||POST|PERF_URL|/api/v1/items/{}||7"},
				{"module:routes.js", parse.PropEnvReads, "PERF_URL"},
			},
		},
		{
			file: "client.ts",
			src: `function apiBase() {
  return process.env.DATA_URL + '/api/v1';
}
const api = axios.create({ baseURL: apiBase() });

export async function apiGet(path: string) {
  return fetch(` + "`${process.env.API_URL}${path}`" + `, { method: 'GET' });
}

export function loadUser(id: string) {
  return apiGet(` + "`/v1/users/${id}`" + `);
}

export async function jobs() {
  let target;
  target = '/jobs';
  const res = await api.post<Job>(target, {});
  return res;
}
`,
			props: []want{
				{"func:client.ts#apiGet", parse.PropHTTPWrapper, "0|GET|API_URL"},
				{"func:client.ts#loadUser", parse.PropHTTPCalls, "wrapper|apiGet|0|GET||/v1/users/{}||11"},
				{"func:client.ts#jobs", parse.PropHTTPCalls, "direct|||POST|APIBASE;DATA_URL|/jobs||17"},
				{"func:client.ts#apiBase", parse.PropEnvReads, "DATA_URL"},
			},
		},
		{
			file: "app/api/users/[id]/route.ts",
			src: `export async function GET(req: Request) { return Response.json({}); }
export const DELETE = async () => new Response(null);
`,
			props: []want{
				{"module:app/api/users/[id]/route.ts", parse.PropNextRoute, "GET,DELETE"},
			},
		},
		{
			file: "service.py",
			src: `import httpx
from fastapi import APIRouter
from .routes import scores_router

router = APIRouter(prefix="/scores")
VOCAB = "/api/v1/vocab"


@router.get(
    "/player/{player_id}")
async def player(player_id: str):
    return {}


class Backend:
    def __init__(self):
        self._http = httpx.AsyncClient(base_url=settings.backend_url)

    async def get(self, endpoint, params=None):
        return await self._http.get(endpoint, params=params)

    async def vocab(self):
        return await self._http.get(VOCAB)


app.include_router(scores_router, prefix="/api/v1")


class Settings(BaseSettings):
    engine_url: str = Field(validation_alias=AliasChoices("AI_ENGINE_URL", "ENGINE_URL"))
`,
			props: []want{
				{"module:service.py", parse.PropHTTPRouters, "router|/scores||router"},
				{"module:service.py", parse.PropHTTPRoutes, "GET|/player/{player_id}|router|player|11"},
				{"module:service.py", parse.PropHTTPMounts, "/api/v1|app|scores_router|.routes.scores_router;.routes"},
				{"module:service.py", parse.PropHTTPImports, "scores_router|scores_router|.routes.scores_router;.routes"},
				{"method:service.py#Backend.get", parse.PropHTTPWrapper, "0|GET|BACKEND_URL;SETTINGS"},
				{"method:service.py#Backend.vocab", parse.PropHTTPCalls, "direct|||GET|BACKEND_URL;SETTINGS|/api/v1/vocab||23"},
				{"module:service.py", parse.PropEnvAliases, "ENGINE_URL=AI_ENGINE_URL"},
			},
		},
	}
	for _, c := range cases {
		path := filepath.Join(root, filepath.FromSlash(c.file))
		mustWrite(t, path, c.src)
		res, err := parse.ParseFile(parse.NewRegistry(), path)
		if err != nil {
			t.Fatal(err)
		}
		fix := func(id string) graph.NodeID { return graph.NodeID(strings.Replace(id, c.file, path, 1)) }
		props := map[graph.NodeID]map[string]string{}
		for _, n := range res.Nodes {
			props[n.ID] = n.Props
		}
		for _, w := range c.props {
			if got := props[fix(w.node)][w.prop]; got != w.value {
				t.Errorf("%s: %s %s = %q, want %q", c.file, w.node, w.prop, got, w.value)
			}
		}
	}
}
