# Drillip

Lightweight, self-hosted error tracking. Receives errors via the Sentry SDK protocol, stores them in SQLite, and notifies you by email when something new breaks.

## Quick start

### Container (recommended)

```bash
docker run -d \
  -v drillip-data:/data \
  -p 127.0.0.1:8300:8300 \
  -e DRILLIP_DB=/data/errors.db \
  -e DRILLIP_ADDR=0.0.0.0:8300 \
  ghcr.io/philhem/drillip:v0.3.2
```

### Binary

```bash
go install github.com/PhilHem/drillip@latest
drillip serve
```

### Systemd

See [`deploy/drillip.service`](deploy/drillip.service) for a hardened unit file.

## Send errors

Point any Sentry SDK at Drillip. The DSN key is ignored — any value works.

```python
# Python
import sentry_sdk
sentry_sdk.init(
    dsn="http://anykey@127.0.0.1:8300/1",
    release="v1.2.0",
    environment="production",
)
```

```javascript
// JavaScript
Sentry.init({
  dsn: "http://anykey@127.0.0.1:8300/1",
  release: "1.2.0",
});
```

```go
// Go
sentry.Init(sentry.ClientOptions{
    Dsn:         "http://anykey@127.0.0.1:8300/1",
    Release:     "v1.2.0",
    Environment: "production",
})
```

## Configuration

All configuration is via environment variables. Nothing is required — Drillip works with zero config.

### Core

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_DB` | `errors.db` | SQLite database path |
| `DRILLIP_ADDR` | `127.0.0.1:8300` | Listen address |
| `DRILLIP_PROJECT` | — | Project name shown in notifications |

### Email notifications

Disabled when `HOST` or `TO` is unset.

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_SMTP_HOST` | — | SMTP server |
| `DRILLIP_SMTP_PORT` | `25` | SMTP port |
| `DRILLIP_SMTP_FROM` | — | Sender address |
| `DRILLIP_SMTP_TO` | — | Recipient address |
| `DRILLIP_SMTP_USER` | — | SMTP username (optional) |
| `DRILLIP_SMTP_PASS` | — | SMTP password (optional) |
| `DRILLIP_SMTP_COOLDOWN` | `60s` | Min interval between emails |
| `DRILLIP_SMTP_DIGEST` | `5m` | Batch window for burst notifications (`0` = immediate) |

### Lifecycle

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_RESOLVE_AFTER` | `24h` | Auto-resolve errors with no occurrences for this duration |
| `DRILLIP_RETAIN` | `90d` | Auto-delete occurrences older than this |

### Integrations (for `correlate`)

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_UNIT` | — | Systemd unit name for journalctl log correlation |
| `DRILLIP_VM_URL` | — | VictoriaMetrics base URL for metrics at time of error |
| `DRILLIP_VT_URL` | — | VictoriaTraces base URL for distributed trace spans |
| `DRILLIP_PYROSCOPE_URL` | — | Pyroscope base URL for CPU profiles |
| `DRILLIP_SERVICE` | — | Service name for Pyroscope queries |

## API

All endpoints return JSON.

### Ingest

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/1/store/` | Ingest a Sentry event (plain JSON) |
| `POST` | `/api/1/envelope/` | Ingest a Sentry envelope (gzip/brotli supported) |

### Query

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/0/top/` | Errors sorted by occurrence count |
| `GET` | `/api/0/recent/?hours=1` | Errors first seen within the last N hours |
| `GET` | `/api/0/show/<fp>/` | Error detail with tag distribution |
| `GET` | `/api/0/trend/<fp>/` | Hourly occurrence histogram (24h) |
| `GET` | `/api/0/releases/<fp>/` | Which releases had this error |
| `GET` | `/api/0/stats/` | Total unique errors and occurrences |
| `GET` | `/api/0/correlate/<fp>/?nth=1` | Full context: stacktrace, logs, metrics, traces, profiles |

Query parameters for `top` and `recent`:
- `?level=error` — filter by severity
- `?tag=key=value` — filter by tag

### Actions

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/0/resolve/<fp>/` | Mark an error as resolved |
| `POST` | `/api/0/gc/?older_than=30d` | Delete occurrences older than duration |
| `POST` | `/api/0/silence/<fp>/?duration=24h&reason=...` | Silence notifications for an error |
| `DELETE` | `/api/0/silence/<fp>/` | Remove a silence |
| `GET` | `/api/0/silences/` | List active silences |

### Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/` or `/-/healthy` | Returns `ok` if the database is reachable |

## CLI

The same binary serves HTTP and provides CLI commands for investigation.

```
drillip top [--level error] [--tag key=value] [--limit 25]
drillip recent [--hours 1] [--level error] [--tag key=value]
drillip show <fingerprint>
drillip trend <fingerprint>
drillip correlate <fingerprint> [--nth 1]
drillip releases <fingerprint>
drillip stats
drillip gc <duration>                  # e.g., 30d, 24h, 2w
drillip resolve <fingerprint>
drillip silence <fingerprint> [duration] [--reason "..."]
drillip silences
drillip unsilence <fingerprint>
drillip health
```

Fingerprints can be abbreviated — `drillip show 04827c` matches the full fingerprint.

## How it works

**Ingestion:** Sentry SDKs POST error events. Drillip parses the envelope, extracts the exception or message, computes a SHA256 fingerprint (from exception type + top stack frame location), and stores it in SQLite. Duplicate fingerprints increment the count.

**Notifications:** New errors and regressions (resolved errors that reappear) trigger email notifications. Emails include the exception, full stacktrace, request URL, user context, breadcrumbs, tags, and CLI commands to investigate further. Multiple errors within the digest window are batched into a single summary email.

**Lifecycle:** Errors that haven't recurred for `DRILLIP_RESOLVE_AFTER` (default 24h) are auto-resolved. If a resolved error reappears, it's flagged as a regression and re-notifies. Occurrences older than `DRILLIP_RETAIN` (default 90d) are automatically pruned.

**Correlation:** The `/api/0/correlate/<fp>/` endpoint (and `drillip correlate` CLI) assembles everything about an error in one response: stacktrace, breadcrumbs, user context, surrounding journalctl logs, system metrics from VictoriaMetrics, distributed trace spans from VictoriaTraces, and CPU profiles from Pyroscope.

## License

MIT
