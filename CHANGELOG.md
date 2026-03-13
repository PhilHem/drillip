# Changelog

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
