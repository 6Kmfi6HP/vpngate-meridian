# VPNGate Scraper API

CommonJS Node.js scraper for VPN Gate data.

The main branch contains only source code and project documentation. Generated VPN data is written to `public/` during CI and published to the `gh-pages` branch as a single forced snapshot, so hourly or scheduled refreshes do not keep adding large data commits to the main branch history.

## Local Usage

Install dependencies:

```bash
npm ci
```

Run a live collection:

```bash
npm start
```

By default the scraper performs 1500 VPN Gate API requests and writes generated files under `public/`:

- `public/json/data.json`
- `public/json/changes.json`
- `public/state/servers.json`
- `public/configs/*.ovpn`
- `public/README.md`

For a smaller local smoke run:

```bash
TOTAL_REQUESTS=5 OUTPUT_DIR=tmp/smoke STATE_PATH=tmp/smoke/state/servers.json npm start
```

On PowerShell:

```powershell
$env:TOTAL_REQUESTS='5'
$env:OUTPUT_DIR='tmp/smoke'
$env:STATE_PATH='tmp/smoke/state/servers.json'
npm start
```

## Incremental Data Model

Each scraped server is assigned a stable ID from its hostname, or from IP and country when the hostname is missing. The scraper reads the previous `state/servers.json` before merging the current collection.

State fields include:

- `firstSeen`
- `lastSeen`
- `lastChanged`
- `seenCount`
- `missCount`
- `status`
- `configHash`
- `contentHash`

Servers are not removed immediately when one scrape misses them. `ACTIVE_MISS_LIMIT` controls when a missing server becomes inactive, and `PRUNE_MISS_LIMIT` controls when long-missing inactive entries are pruned from state.

## GitHub Actions

The workflow runs tests on push and pull request. On push, manual dispatch, and schedule it also runs a live scrape, restores the previous state from `gh-pages`, generates a new output snapshot, uploads it as an artifact, and force-pushes the generated files to `gh-pages`.

Default CI behavior:

- Push or manual dispatch: 100 live requests by default.
- Schedule: 1500 live requests every 6 hours.
- Generated data is not committed to `master` or `main`.

## Test

```bash
npm test
```
