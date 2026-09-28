# Drillip

Lightweight, self-hosted error tracking. Receives errors via the Sentry SDK protocol, stores them in SQLite, and notifies you by email when something new breaks.

## Quick start

New to Drillip? Follow [Capture and resolve your first error](docs/tutorials/first-error.md)
to start Drillip, send an event, and check its lifecycle.

### Container (recommended)

```bash
docker run -d \
  -v drillip-data:/data \
  -p 127.0.0.1:8300:8300 \
  -e DRILLIP_DB=/data/errors.db \
  -e DRILLIP_ADDR=0.0.0.0:8300 \
  ghcr.io/philhem/drillip:v0.3.4
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

See the [configuration reference](docs/reference/configuration.md) for environment
variables, defaults, accepted values, and command-line overrides.

To enable and test email delivery, follow
[Set up and check email notifications](docs/how-to/email-notifications.md).

## API

All endpoints return JSON. Error responses use `{"error": "message"}` with appropriate HTTP status codes.

### Ingest

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/1/store/` | Ingest a Sentry event (plain JSON) |
| `POST` | `/api/1/envelope/` | Ingest a Sentry envelope (gzip/brotli supported, 10MB limit) |

Events are sanitized at ingest: oversized fields are truncated, invalid levels normalized, CRLF stripped from exception types.

### Query

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/0/top/` | Errors sorted by occurrence count |
| `GET` | `/api/0/recent/?hours=1` | Errors first seen within the last N hours (max 8760) |
| `GET` | `/api/0/show/<fp>/` | Error detail with tag distribution |
| `GET` | `/api/0/trend/<fp>/` | Hourly occurrence histogram (24h) |
| `GET` | `/api/0/releases/<fp>/` | Which releases had this error |
| `GET` | `/api/0/stats/` | Total unique errors and occurrences |
| `GET` | `/api/0/correlate/<fp>/?nth=1` | Full context: stacktrace, logs, metrics, traces, profiles |

Query parameters for `top` and `recent`:
- `?level=error` — filter by severity
- `?tag=key=value` — filter by tag

Fingerprints can be abbreviated — `/api/0/show/04827c/` matches the full fingerprint. Only lowercase hex characters (a-f, 0-9) are accepted.

Responses include a `state` field: `new` (first seen within the last hour), `ongoing`, or `resolved`.

### Actions

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/0/resolve/<fp>/` | Mark an error as resolved |
| `POST` | `/api/0/gc/?older_than=30d` | Delete occurrences older than duration |
| `POST` | `/api/0/silence/<fp>/?duration=24h&reason=...` | Silence notifications for an error |
| `DELETE` | `/api/0/silence/<fp>/` | Remove a silence |
| `GET` | `/api/0/silences/` | List active silences |
| `POST` | `/api/0/test-email/` | Send a test email to verify SMTP configuration |

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

**Ingestion:** Sentry SDKs POST error events. Drillip parses the envelope, extracts the exception or message, sanitizes fields, computes a SHA256 fingerprint (from exception type + top stack frame location), and stores it in SQLite. Duplicate fingerprints increment the count.

**Notifications:** New errors and regressions (resolved errors that reappear) trigger email notifications. Emails include the exception, full stacktrace, request URL, user context, breadcrumbs, tags, and CLI commands to investigate further. Multiple errors within the digest window are batched into a single summary email. Failed sends are retried with exponential backoff. Silenced fingerprints are skipped.

**Lifecycle:** Errors that haven't recurred for `DRILLIP_RESOLVE_AFTER` (default 24h) are auto-resolved. If a resolved error reappears, it's flagged as a regression and re-notifies with "was resolved for X" context. Occurrences older than `DRILLIP_RETAIN` (default 90d) are automatically pruned.

**Correlation:** The `/api/0/correlate/<fp>/` endpoint assembles everything about an error in one response: stacktrace, breadcrumbs, user context, surrounding journalctl logs, system metrics from VictoriaMetrics, distributed trace spans from VictoriaTraces, and CPU profiles from Pyroscope. Each section is omitted when the integration isn't configured.

## Source layout

The source follows the `ch init --template hombergs-go` layout with explicit
port packages and application services:

```text
main.go                         process signals and exit status
internal/
  domain/                       events, error models, fingerprints, query results
  application/
    port/in/                    use-case interfaces (package inport)
    port/out/                   persistence, notification, and telemetry interfaces
    service/                    ingestion, error management, correlation, maintenance
  adapter/
    in/api/                     JSON HTTP API
    in/ingest/                  Sentry HTTP protocol
    in/cli/                     command parsing and output
    out/sqlite/                 storage and migrations
    out/smtp/                   email delivery and formatting
    out/observability/          journalctl, VictoriaMetrics, VictoriaTraces, Pyroscope
  bootstrap/                    configuration, wiring, and server lifecycle
```

Services coordinate domain models through outbound ports. Inbound adapters call
inbound ports; they do not import services or concrete outbound adapters.
The domain and both port packages are independent of services and adapters.
`bootstrap` constructs the concrete dependencies. It is an explicit optional
directory in the template so the existing root executable stays installable.
Tests live beside the code they exercise. Deployment files live in `deploy/`.

`components.yaml` declares the layers and applies the `hombergs-go` structure
template to `internal/`. Run `ch structure-check .` to check directories.
`go test ./...` also checks production imports against the layer rules; this
check does not depend on the current coverage of `ch`'s Go import graph.
Integration tests may connect concrete adapters and services.

Build and install the executable from the repository root with `go build .` or
`go install .`. The published install command remains
`go install github.com/PhilHem/drillip@latest`. Application packages are internal
and cannot be imported by other projects.

## License

MIT
