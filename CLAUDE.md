# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Additional reference: [AGENTS.md](./AGENTS.md) contains coding style, commit guidelines, and project conventions.

## Commands

```bash
go build -o vpn-meridian ./cmd/vpn-meridian/       # Build the binary
go run ./cmd/vpn-meridian/                          # Run the scraper
go vet ./...                                         # Lint check
go test ./...                                        # Run tests
```

### Smoke test (local, small run)

```bash
TOTAL_REQUESTS=5 OUTPUT_DIR=tmp/smoke STATE_PATH=tmp/smoke/state/servers.json go run ./cmd/vpn-meridian/
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

**Pure Go project** with Python post-processing for MaxMind enrichment.

### Scraper (`cmd/vpn-meridian/` + `internal/`)

- **cmd/vpn-meridian/main.go** — CLI entry point. Orchestrates scraping, state management, and file output.
- **internal/config/** — Loads environment variables into a typed `Config` struct.
- **internal/scraper/** — HTTP client with randomized headers, worker pool for concurrent requests.
- **internal/csvparser/** — Parses VPN Gate CSV API response into `Server` structs.
- **internal/state/** — Incremental state model. Merges current scrape with previous state, tracks lifecycle.
- **internal/output/** — File writers: JSON, HTML (via embedded template), README, VPN configs.
- **internal/maxmind/** — MaxMind GeoLite2 enrichment (GeoIP country/city/ASN annotations).
- **internal/mihomo/** — mihomo YAML proxy config generation.

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

- `validate` job: Go build + vet (runs on push + PR)
- `collect` job: restores state from `gh-pages`, runs scraper, downloads MaxMind DBs from external repo, enriches, validates, uploads artifact, force-pushes to `gh-pages` branch
- `test` job: builds Go tester, tests OpenVPN servers with mihomo
- Schedule: every 6 hours (200 requests). Manual dispatch and push: 100 requests.

## Important Notes

- **Generated output goes to `gh-pages`, not main branch.** The `public/` directory is gitignored on main. Do not commit generated files to main.
- **No build step required for development.** This is pure Go — `go run ./cmd/vpn-meridian/` runs directly.
- **The generated `index.html` is a full client-side SPA** with inline JavaScript that fetches `data.json` at runtime, supports filtering/searching/sorting, and updates metrics dynamically.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `TOTAL_REQUESTS` | 1500 | Number of API calls |
| `WORKER_COUNT` | 8 | Max worker goroutines |
| `OUTPUT_DIR` | public | Output directory |
| `STATE_PATH` | public/state/servers.json | Incremental state file |
| `ACTIVE_MISS_LIMIT` | 12 | Misses before server goes inactive |
| `PRUNE_MISS_LIMIT` | 48 | Misses before pruning from state |
| `REQUEST_TIMEOUT_MS` | 30000 | Per-request timeout |
| `NO_PROGRESS` | (unset) | Set to "1" to hide progress |
