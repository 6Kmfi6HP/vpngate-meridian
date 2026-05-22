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

By default the scraper performs 1500 VPN Gate API requests locally and writes generated files under `public/`:

- `public/json/data.json`
- `public/json/changes.json`
- `public/json/data.maxmind.json` when MaxMind export is enabled
- `public/state/servers.json`
- `public/configs/*.ovpn`
- `public/mihomo_openvpn.yaml` when MaxMind export is enabled
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

## MaxMind and mihomo Export

The scraper output can be enriched with MaxMind GeoLite2 Country, City, and ASN databases, then exported as a mihomo OpenVPN provider file:

```bash
python -m pip install -r requirements-maxmind.txt
python scripts/enrich_maxmind.py \
  --input public/json/data.json \
  --output public/json/data.maxmind.json \
  --mihomo-output public/mihomo_openvpn.yaml \
  --maxmind-dir maxmind
```

The MaxMind export is a metadata enrichment step. It does not replace `public/json/data.json`, does not change the incremental state model, does not connect to VPN servers, and does not claim VPN/proxy risk scoring beyond the GeoLite2 fields available in the databases.

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
- `openvpn_configdata_base64` while the server is still publishable

The VPN Gate API can return a changing sample instead of a complete global list, so the public output is a passive rolling window, not just the latest response. Servers seen in the current collection are published as `active`; servers missed by the current scrape stay published as `missing` while their consecutive miss count is below `ACTIVE_MISS_LIMIT`. After that threshold they become `inactive` and are removed from `README.md`, `json/data.json`, and `configs/*.ovpn`. `PRUNE_MISS_LIMIT` controls when long-missing inactive entries are pruned from state entirely.

State keeps the last known OpenVPN config only while a server is still publishable as `active` or `missing`. Once it becomes `inactive`, `state/servers.json` drops `openvpn_configdata_base64` and keeps only lifecycle metadata and hashes until the entry is pruned. This allows the rolling window to accumulate servers from random API samples without keeping stale configs forever.

The scraper does not actively test, ping, connect to, or speed-test VPN server IPs. Reported `ping` and `speed` values come from the VPN Gate API payload and should be treated as source-reported metadata.

## GitHub Actions

The workflow runs Node.js and Python tests on push and pull request. On push, manual dispatch, and schedule it also runs a live scrape, restores the previous state from `gh-pages`, generates a new output snapshot, enriches it with MaxMind data, uploads it as an artifact, and force-pushes the generated files to `gh-pages`.

Default CI behavior:

- Push or manual dispatch: 100 live requests by default.
- Schedule: 200 live requests every 6 hours.
- CI performs passive API collection only; it does not actively probe or connect to VPN server IPs.
- With the default `ACTIVE_MISS_LIMIT=12` and 6-hour schedule, a server must be missed for roughly 3 days before it leaves the public output. With `PRUNE_MISS_LIMIT=48`, stale state is removed after roughly 12 days of consecutive misses.
- Generated data is not committed to `master` or `main`.

## Test

```bash
npm test
python -m pip install -r requirements-maxmind.txt
python -m pytest -q
```
