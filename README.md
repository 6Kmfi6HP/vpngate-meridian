# VPN Meridian

Global VPN server aggregator that collects, validates, and publishes free OpenVPN configurations with a real-time web dashboard.

> **Meridian** /mr.di.n/ — a great circle passing through the poles; a line connecting points of equal value on a map. VPN Meridian maps the world's free VPN infrastructure into a single, navigable index.

## What It Does

VPN Meridian is an automated pipeline that:

1. **Collects** free VPN server data from the VPN Gate API using concurrent workers
2. **Tracks** server state incrementally — new, active, missing, inactive, pruned
3. **Enriches** with MaxMind GeoLite2 GeoIP data (country, city, ASN)
4. **Generates** OpenVPN configs, mihomo proxy YAML, and a client-side web dashboard
5. **Publishes** everything to GitHub Pages on a 6-hour schedule

The web dashboard (`index.html`) is a self-contained SPA — no build step, no framework, no server. It fetches `data.json` at runtime and supports filtering, searching, and sorting across 20+ fields.

## Quick Start

```bash
# Build
go build -o vpn-meridian ./cmd/vpn-meridian/

# Run (default: 1500 API requests)
./vpn-meridian

# Smoke test (5 requests)
TOTAL_REQUESTS=5 OUTPUT_DIR=tmp/smoke STATE_PATH=tmp/smoke/state/servers.json go run ./cmd/vpn-meridian/
```

## Architecture

```
cmd/vpn-meridian/        CLI entry point
internal/
  config/                Environment variable configuration
  scraper/               HTTP client + worker pool
  csvparser/             VPN Gate CSV response parser
  state/                 Incremental state management
  output/                File writers (JSON, HTML, README, VPN configs)
  maxmind/               GeoLite2 enrichment
  mihomo/                mihomo YAML proxy config generation
scripts/                 Python post-processing (MaxMind enrichment)
```

### Incremental State Model

Each server gets a stable identity from hostname (preferred), IP+country, or config hash. The lifecycle:

```
new -> active -> missing (config kept) -> inactive (config dropped) -> pruned
```

- `ACTIVE_MISS_LIMIT` (default 12): ~3 days of missed scrapes before a server goes inactive
- `PRUNE_MISS_LIMIT` (default 48): ~12 days before stale state is removed entirely
- Content changes detected via `contentHash`; config identity via `configHash`

### MaxMind Enrichment

```bash
python -m pip install -r requirements-maxmind.txt
python scripts/enrich_maxmind.py \
  --input public/json/data.json \
  --output public/json/data.maxmind.json \
  --mihomo-output public/mihomo_openvpn.yaml \
  --maxmind-dir maxmind
```

Or use the built-in Go enrichment:

```bash
go run ./cmd/vpn-meridian/ enrich \
  --input public/json/data.json \
  --output public/json/data.maxmind.json \
  --mihomo-output public/mihomo_openvpn.yaml \
  --maxmind-dir maxmind
```

## Output Files

| File | Description |
|------|-------------|
| `public/json/data.json` | Full server dataset with metadata |
| `public/json/data.maxmind.json` | GeoIP-enriched server dataset |
| `public/json/changes.json` | Diff of state transitions since last scrape |
| `public/state/servers.json` | Incremental state file |
| `public/configs/*.ovpn` | Individual OpenVPN config files |
| `public/mihomo_openvpn.yaml` | mihomo proxy provider config |
| `public/index.html` | Client-side web dashboard |

## CI/CD

GitHub Actions (`.github/workflows/main.yml`):

- **validate** — Go build + vet on every push and PR
- **collect** — Live scrape + MaxMind enrichment + publish to `gh-pages`
- **test** — Builds OpenVPN tester, validates servers with mihomo
- **Schedule** — Every 6 hours (200 requests). Manual dispatch: 100 requests.

Generated output is force-pushed to the `gh-pages` branch. Configure GitHub Pages to serve from that branch.

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `TOTAL_REQUESTS` | 1500 | Number of API calls |
| `WORKER_COUNT` | 8 | Max worker goroutines |
| `OUTPUT_DIR` | `public` | Output directory |
| `STATE_PATH` | `public/state/servers.json` | Incremental state file |
| `ACTIVE_MISS_LIMIT` | 12 | Misses before server goes inactive |
| `PRUNE_MISS_LIMIT` | 48 | Misses before pruning from state |
| `REQUEST_TIMEOUT_MS` | 30000 | Per-request timeout |
| `NO_PROGRESS` | (unset) | Set to `1` to hide progress bar |

## Development

```bash
go build -o vpn-meridian ./cmd/vpn-meridian/  # Build
go run ./cmd/vpn-meridian/                    # Run
go vet ./...                                  # Lint
go test ./...                                 # Tests
```

No build step required — pure Go with `embed` for the HTML template.

## License

See [LICENSE](./LICENSE) for details.
