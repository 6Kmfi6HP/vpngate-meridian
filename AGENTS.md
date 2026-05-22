# Repository Guidelines

## Project Structure & Module Organization

This is a CommonJS Node.js scraper for VPN Gate data. The entry point is `index.js`, which starts worker threads, collects VPN data, deduplicates servers, and writes generated outputs. Core scraping/parsing logic lives in `lib/VpnScraper.js`; `lib/main.js` is an older single-fetch helper. File generation utilities are in `utils/fileHandler.js`, and request randomization helpers are in `utils/randomizer.js`.

Generated artifacts are part of the repository output: `configs/` contains OpenVPN `.ovpn` files, `json/data.json` stores structured results, and `README.md` is regenerated as the public server list.

## Build, Test, and Development Commands

- `npm install`: install runtime dependencies from `package-lock.json`.
- `npm start`: run `node index.js`; fetches live VPN Gate data, rewrites `configs/`, updates `json/data.json`, and regenerates `README.md`.
- `node index.js`: direct equivalent of `npm start` for local debugging.

There is no build step. The GitHub Actions workflow runs on Node `14.x`, removes and recreates `configs/`, runs `npm install --if-present`, then runs `npm start`.

## Coding Style & Naming Conventions

Use CommonJS modules with `require` and `module.exports`. Keep JavaScript indentation consistent with the current codebase: four spaces inside blocks, semicolons, and single quotes unless matching existing double-quoted code. Use PascalCase for classes such as `VpnScraper` and `FileHandler`; use camelCase for functions, variables, and object properties.

Keep generated-file behavior centralized in `utils/fileHandler.js`. Avoid hard-coding output paths elsewhere unless adding a deliberate new artifact.

## Testing Guidelines

No automated test framework is currently configured. For scraper or parser changes, add focused tests under a new `test/` directory with sample VPN Gate CSV payloads. Until tests exist, use `npm start` as a smoke test and verify that `json/data.json`, `configs/*.ovpn`, and `README.md` are updated.

## Commit & Pull Request Guidelines

This checkout has no existing commit history to infer a convention from. Use short, imperative commit messages such as `Update scraper parsing` or `Add parser tests`. The workflow’s automated data refresh uses `Update Data <timestamp>`.

Pull requests should describe the behavior changed, note whether live VPN Gate data was fetched, and list regenerated files.

## Security & Configuration Tips

Treat `.ovpn` files and live scraped endpoints as generated external data. Do not add secrets, private proxy credentials, or local-only network assumptions. If proxy support is re-enabled, keep configuration explicit and avoid committing machine-specific values.

## Agent-Specific Instructions

Cap unknown or potentially large command output before returning it, for example by limiting file lists and generated data previews to a few kilobytes.
