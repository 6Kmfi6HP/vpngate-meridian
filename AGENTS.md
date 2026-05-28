# AGENTS.md

> Guidelines for AI coding agents working on this repository.

## Project Overview

VPN Gate scraper that collects free VPN server data, enriches with GeoIP information, and generates web output. **Pure Go project** with Python post-processing for MaxMind enrichment.

## Build & Test Commands

```bash
go build -o vpngate-scraper ./cmd/vpngate-scraper/  # Build
go vet ./...                                         # Lint
go run ./cmd/vpngate-scraper/                        # Run
```

## Coding Standards

- **Language**: Go (standard library + minimal dependencies)
- **Formatting**: `gofmt` (enforced by toolchain)
- **Error handling**: Always check errors; use `fmt.Errorf("context: %w", err)` for wrapping
- **Testing**: Use `testing` package with table-driven tests
- **Imports**: Group standard library, then external packages, then internal packages
- **Naming**: Follow Go conventions (camelCase, exported PascalCase)
- **Comments**: Only when the "why" is non-obvious

## Architecture

```
cmd/vpngate-scraper/     # CLI entry point
internal/
  config/                # Environment variable configuration
  scraper/               # HTTP client + worker pool
  csvparser/             # VPN Gate CSV response parser
  state/                 # Incremental state management
  output/                # File writers (JSON, HTML, README)
  maxmind/               # GeoLite2 enrichment
  mihomo/                # mihomo YAML generation
```

## Commit Messages

- Format: `<type>(<scope>): <description>`
- Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`
- Keep descriptions under 72 characters
- Reference issues where relevant

## Environment Variables

All configuration via environment variables, loaded in `internal/config/config.go`. See CLAUDE.md for full list.

## CI/CD

GitHub Actions workflow at `.github/workflows/main.yml`:
- `validate`: Go build + vet
- `collect`: Run scraper, enrich with MaxMind, upload artifact
- `test`: Build OpenVPN tester, test servers, publish to gh-pages
- Schedule: every 6 hours

## Important Notes

- **Generated output goes to `gh-pages` branch**, not main
- **MaxMind enrichment** currently uses Python scripts (transitional)
- **State file** is critical for incremental scraping - never delete without understanding lifecycle
- **Atomic writes** for all output files (temp + rename)
