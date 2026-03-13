# Changelog

## v0.2.0 - 2026-03-14

### Added

- HTTP query API for remote access without CLI or container exec: `GET /api/0/top/`, `/api/0/recent/`, `/api/0/show/<fp>/`, `/api/0/trend/<fp>/`, `/api/0/releases/<fp>/`, `/api/0/stats/`, `POST /api/0/gc/`
- Tag-based filtering: `--tag key=value` on CLI commands and `?tag=key=value` on API endpoints narrow results by any Sentry SDK tag (e.g. server, endpoint, region)
- `show` command and `/api/0/show/` display tag distribution across occurrences (e.g. "server: web-1 80%, web-2 20%")
- Ingest response now returns the stored fingerprint (`{"id":"04827c09..."}`) instead of a static `"ok"`, enabling callers to correlate events

### Changed

- Graceful shutdown now checkpoints the WAL, so `errors.db` is self-contained for backups and ad-hoc copies without needing the `-wal`/`-shm` files

### Fixed

- Upgrading from older schema versions no longer crashes — missing columns (`level`, `tags`) are auto-migrated on startup

## v0.1.3 - 2026-03-13

### Fixed

- GHCR image tags now include the `v` prefix (e.g. `v0.1.3` not `0.1.3`) matching git tag convention
- Added `drillip-data.volume` quadlet file so Podman systemd resolves the volume dependency by filename
- Root path (`/`) now returns health status instead of 404, so generic readiness probes work without knowing `/-/healthy`

## v0.1.2 - 2026-03-13

### Added

- Sentry `capture_message()` events are now ingested alongside exceptions, with severity level tracking (fatal, error, warning, info, debug)
- `top` and `recent` commands support `--level` filter to show only errors, warnings, or info messages
- Systemd service unit (`drillip.service`) for bare-metal deployment with sandboxing and health check

## v0.1.1 - 2026-03-13

### Fixed

- CLI commands now accept `-db` and `-addr` flags before the subcommand (e.g. `drillip -db /path/to/db top`), not just environment variables

## v0.1.0 - 2026-03-13

### Added

- Sentry SDK compatible HTTP endpoint for ingesting errors (envelope and plain JSON formats)
- SQLite storage with WAL mode, fingerprint-based deduplication, and occurrence tracking
- CLI subcommands for error investigation: `top`, `recent`, `show`, `trend`, `releases`, `stats`, `gc`, `correlate`, `health`
- Each CLI command suggests the next drill-down step in its output
- Optional integrations with journalctl, VictoriaTraces, VictoriaMetrics, and Pyroscope for correlated debugging
- `correlate` command assembles stacktrace, logs, breadcrumbs, traces, metrics, and profiles in one view
- ASCII histogram for error occurrence trends over the last 24 hours
- Garbage collection command for pruning old occurrences
- Container image published to GHCR on tag push
