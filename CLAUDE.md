# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Additional reference: [AGENTS.md](./AGENTS.md) contains coding style, commit guidelines, and project conventions.

## Commands

```bash
npm ci                  # Install dependencies (prefer over npm install)
npm start               # Run scraper (default: 1500 requests, output to public/)
npm test                # Node.js syntax check + fileHandler incremental state tests
npm test -- --verbose   # Show test assertions
python -m pytest -q     # Python MaxMind enrichment tests
```

### Smoke test (local, small run)

```bash
TOTAL_REQUESTS=5 OUTPUT_DIR=tmp/smoke STATE_PATH=tmp/smoke/state/servers.json npm start
```

### MaxMind enrichment

```bash
python -m pip install -r requirements-maxmind.txt
python scripts/enrich_maxmind.py \
  --input public/json/data.json \
  --output public/json/data.maxmind.json \
  --mihomo-output public/mihomo_openvpn.yaml \
  --maxmind-dir maxmind
```

## Architecture

**Two-language project**: Node.js (scraper) + Python (post-processing enrichment).

### Scraper (`index.js` + `lib/`)

- **index.js** — dual role: entry point AND worker thread code (uses `isMainThread` branching). Main thread distributes requests across worker threads (max `WORKER_COUNT`, default 8), collects results, then calls `FileHandler` to merge, deduplicate, and write output. Worker threads each run `VpnScraper.fetchVpnData()` in a loop.
- **lib/VpnScraper.js** — fetches from `vpngate.net/api/iphone/` with randomized headers/params, parses CSV response into server objects. Proxy agent (SOCKS5) is currently commented out.
- **lib/main.js** — legacy single-fetch helper. Not used by the current scraper.
- **utils/fileHandler.js** — incremental state model (`FileHandler` class). Reads previous `state/servers.json`, merges current collection, tracks lifecycle (firstSeen, lastSeen, missCount, contentHash). Configurable `ACTIVE_MISS_LIMIT` (default 12) and `PRUNE_MISS_LIMIT` (default 48). Outputs: `public/json/data.json`, `public/json/changes.json`, `public/configs/*.ovpn`, `public/index.html`, `public/README.md`, `public/state/servers.json`.
- **utils/randomizer.js** — random User-Agent, cookie, and string generators.

**Key dedup logic**: `buildServerIdentity()` creates a stable server ID from hostname first, falls back to ip+country, then to config hash. When two raw entries produce the same ID, `selectPreferredServer()` keeps the one with higher speed.

### Incremental State Model

Each server identified by stable ID (from hostname, or ip+country, or config hash). Lifecycle:
`new → active → missing (config kept) → inactive (config dropped) → pruned (removed from state)`

Servers not seen in current scrape increment `missCount`. When `missCount >= ACTIVE_MISS_LIMIT` they become inactive (config deleted from state). When `missCount >= PRUNE_MISS_LIMIT` they're pruned entirely. Previous `data.json` snapshot hydrates configs for state entries that don't store them.

**Hash distinction**: `contentHash` (full server metadata, used to detect changes) vs `configHash` (just the OpenVPN config, used for config identity). A server is "updated" only when contentHash changes.

### Changes tracking (`changes.json`)

Each scrape produces a diff of the state transition: `added`, `updated`, `recovered` (missing→active), `missing` (active→missing), `inactive` (missing→inactive), `pruned` (removed from state entirely), `unchangedCount`.

### MaxMind (`scripts/` + `tests/`)

- **scripts/enrich_maxmind.py** — reads `data.json`, annotates servers with GeoLite2 Country/City/ASN data, generates `mihomo_openvpn.yaml`. Uses atomic writes via temp files.
- **tests/test_enrich_maxmind.py** — pytest tests that load the script as a module.

### CI/CD (`.github/workflows/main.yml`)

- `validate` job: Node.js tests + Python tests (runs on push + PR)
- `collect` job: restores state from `gh-pages`, runs scraper, downloads MaxMind DBs from external repo, enriches, validates, uploads artifact, force-pushes to `gh-pages` branch
- Schedule: every 6 hours (200 requests). Manual dispatch and push: 100 requests.

## Important Notes

- **Generated output goes to `gh-pages`, not main branch.** The `public/` directory is gitignored on main. Do not commit generated files to main.
- **No build step.** This is pure Node.js — `node index.js` runs directly.
- **Node tests use raw `assert`, no test framework.** Python tests use pytest.
- **The generated `index.html` is a full client-side SPA** with inline JavaScript that fetches `data.json` at runtime, supports filtering/searching/sorting, and updates metrics dynamically.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `TOTAL_REQUESTS` | 1500 | Number of API calls |
| `WORKER_COUNT` | 8 | Max worker threads |
| `OUTPUT_DIR` | public | Output directory |
| `STATE_PATH` | public/state/servers.json | Incremental state file |
| `ACTIVE_MISS_LIMIT` | 12 | Misses before server goes inactive |
| `PRUNE_MISS_LIMIT` | 48 | Misses before pruning from state |
| `REQUEST_TIMEOUT_MS` | 30000 | Per-request timeout |
| `NO_PROGRESS` | (unset) | Set to "1" to hide progress bars |
