# Changelog

## v0.3.1 - 2026-03-16

### Fixed

- SMTP headers are sanitized to prevent header injection via crafted exception types (#6)
- Request body limited to 10MB to prevent memory exhaustion from oversized payloads (#7)
- Race condition in notification digest buffer fixed — concurrent sends no longer risk panics (#8)
- API resolve and silence handlers now use atomic store operations instead of raw multi-statement SQL (#9)
- Wildcard CORS header removed — Drillip is localhost-only and doesn't need cross-origin access (#10)

### Added

- Events are sanitized at ingest — oversized fields (message, breadcrumbs, stacktrace frames, tags) are truncated to safe limits (#11)
- Shared query logic centralized — tag distribution, prefix lookup, state derivation, and duration parsing live in one place instead of being duplicated across API and CLI (#12)
- API and ingest errors now return structured JSON (`{"error": "message"}`) instead of plain text (#13)
- Failed email sends are retried up to 3 times with exponential backoff (#14)
- Invalid or partial configuration is detected at startup with clear warnings (#15)
- Logging migrated to structured format (`log/slog`) with key-value fields (#16)
- SQLite connection pool tuned for WAL mode concurrency (#17)

## v0.3.0 - 2026-03-16

### Added

- Errors now have a lifecycle — new, ongoing, resolved, regressed — with auto-resolve after 24h of inactivity (`DRILLIP_RESOLVE_AFTER`), manual resolve via `drillip resolve <fp>` or `POST /api/0/resolve/<fp>/` ([#1](https://github.com/PhilHem/drillip/issues/1))
- Regression detection — when a resolved error reappears, Drillip sends an amber-styled notification with "was resolved for X" context instead of treating it as a duplicate ([#2](https://github.com/PhilHem/drillip/issues/2))
- Notification cooldown — emails are rate-limited per fingerprint and globally (`DRILLIP_SMTP_COOLDOWN`, default 60s) to prevent email storms during bad deploys ([#3](https://github.com/PhilHem/drillip/issues/3))
- Digest batching — multiple new errors within a window (`DRILLIP_SMTP_DIGEST`, default 5m) are summarized into a single email instead of individual notifications ([#4](https://github.com/PhilHem/drillip/issues/4))
- Silencing — mute notifications for specific errors via `drillip silence/unsilence` or `POST/DELETE /api/0/silence/<fp>/`, with optional duration and reason ([#5](https://github.com/PhilHem/drillip/issues/5))
- State column in `drillip top` and `drillip recent` output shows whether each error is new, ongoing, or resolved
- `drillip silences` lists active silences with expiry and reason

### Changed

- Codebase reorganized into domain/, store/, ingest/, api/, notify/, cli/, integrations/ packages for navigability — global DB variable replaced with Store struct

## v0.2.2 - 2026-03-16

### Added

- Email notifications on new errors — when a previously unseen error is ingested, Drillip sends an email via SMTP. Configured with `DRILLIP_SMTP_HOST`, `DRILLIP_SMTP_PORT`, `DRILLIP_SMTP_FROM`, `DRILLIP_SMTP_TO`, and optional `DRILLIP_SMTP_USER`/`DRILLIP_SMTP_PASS`. Disabled when host or recipient is unset.

### Changed

- Configuration is now grouped by concern: integration settings (journalctl, VictoriaMetrics, VictoriaTraces, Pyroscope) are in their own struct rather than flat on the top-level config

## v0.2.1 - 2026-03-16

### Fixed

- Events from sentry-sdk 2.54+ (Python/Django) are now accepted — Brotli-compressed envelopes (`Content-Encoding: br`) were previously rejected with 400

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
