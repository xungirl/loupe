# Loupe web UI

React 19 + TypeScript + Vite. No UI kit, no chart library, no state library: function components, hooks,
`react-router-dom`, hand-rolled SVG, and five plain CSS files with variables for the theme.

## Run

```bash
# 1. a server with data (from the repo root)
make build && bin/loupe demo-seed -db data/demo.db
bin/loupe serve -db data/demo.db -addr 127.0.0.1:7171 -demo-live

# 2. the UI (from web/)
npm install
npm run dev          # http://localhost:5174, /api is proxied to :7171 (override with LOUPE_API=...)
npm run build        # tsc -b (strict) + vite build → web/dist, which `bin/loupe serve -web web/dist` serves
npm test             # Vitest unit tests (jsdom)
npm run e2e          # build, then Playwright against a fresh demo DB on :7171 (e2e/start-server.sh)
npm run screenshots  # build, then write docs/screenshots/*.png (1440×900, light)
```

`npm run e2e` / `screenshots` start their own server via `e2e/start-server.sh` (builds `bin/loupe`, re-seeds
`node_modules/.cache/loupe-e2e/demo.db`, serves `web/dist` with `-demo-live`), so port 7171 must be free. Set `LOUPE_REUSE=1` to use a server you already
started on 7171 instead.

## Layout

```
src/
  api.ts                typed client + payload types (all times are Unix ns)
  format.ts             ns → "3.4 s", 1.2M tokens, $ cost, relative time
  useApi.ts             tiny fetch hook: {data, error, loading, reload}
  journey.ts            the /journey stages (edit the text here)
  components/           Layout (sidebar, health dot, theme toggle), ui bits, BarChart, Sparkline, Logo
  trace/                waterfallLayout.ts + treeLayout.ts (pure layout math, unit-tested),
                        Waterfall, SpanPanel, AgentTree, FindingsList
  pages/                one file per route
  styles/               theme.css (variables) · layout · components · trace · pages
  fixtures/             experiment-dryrun.json — a FAKE report, used only by unit tests
e2e/                    app.spec.ts (behaviour), screenshots.spec.ts, start-server.sh
```

## Decisions

- **URL holds the view state** (filters, page, tab, `?finding=`, `?agent=`), so every view is linkable and the
  back button works. Local UI state (selected span, collapsed rows) stays in `useState`.
- **Waterfall** uses the server's depth-first order as-is. Collapsing hides "the following rows with greater depth"
  in one pass; bar `left/width` are percentages of the trace window (with a minimum width for instant events).
  No virtualisation: fine for hundreds of spans; very large imported sessions would want it.
- **Agent tree** is a simplified Reingold–Tilford layout: leaves take consecutive columns, parents centre over their
  children. Children are rebuilt from `parent_key`; unknown parents become extra roots.
- **Finding badges in the sessions table**: trace summaries only carry `finding_count`, so the page loads
  `/api/v1/findings?limit=5000` once and groups by trace.
- **Live tail**: one `EventSource` per trace filter, closed in the effect cleanup. Pause buffers incoming spans and
  flushes them on resume; spans/sec is a 5 s sliding window; the list is capped at 500 rows.
- **Experiments**: every report field is optional. A prominent DRY RUN banner shows when `provider == "fake"` or
  `is_dry_run`. The planning estimate is always labelled as a planning figure, not a measurement.
- Light theme by default; the sidebar toggle switches to dark (stored in `localStorage`).
- Playwright is pinned to 1.55.0 because that version uses the already-cached `chromium-1187`.
