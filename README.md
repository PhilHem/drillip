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

## Documentation

| Need | Documentation |
|---|---|
| Learn with a tutorial | [Capture and resolve your first error](docs/tutorials/first-error.md) |
| Complete a task | [Set up and check email notifications](docs/how-to/email-notifications.md) |
| Look up a setting or interface | [Configuration](docs/reference/configuration.md), [HTTP API](docs/reference/http-api.md), [CLI](docs/reference/cli.md) |
| Understand the concepts | [Error grouping and lifecycle](docs/explanation/error-lifecycle.md) |

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

## How it works

**Ingestion:** Sentry SDKs POST error events. Drillip parses the envelope, extracts the exception or message, sanitizes fields, computes a fingerprint from the exception type and stack frame location or from the message text, and stores it in SQLite. Duplicate fingerprints increment the count.

**Notifications:** New errors and regressions (resolved errors that reappear) trigger email notifications. Emails include the exception, full stacktrace, request URL, user context, breadcrumbs, tags, and CLI commands to investigate further. Multiple errors within the digest window are batched into a single summary email. Failed sends are retried with exponential backoff. Silenced fingerprints are skipped.

**Lifecycle:** Errors can be resolved manually or after a period without occurrences.
A matching event after resolution reopens the error as a regression. Retention
removes old occurrences while preserving the grouped error and its total count.
See [How Drillip groups errors and tracks their lifecycle](docs/explanation/error-lifecycle.md)
for grouping rules, states, and the relationship between resolution and retention.

**Correlation:** The `/api/0/correlate/<fp>/` endpoint assembles everything about an error in one response: stacktrace, breadcrumbs, user context, surrounding journalctl logs, system metrics from VictoriaMetrics, distributed trace spans from VictoriaTraces, and CPU profiles from Pyroscope. Each section is omitted when the integration isn't configured.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build and check commands and the
[architecture explanation](docs/explanation/architecture.md) for the source layout
and dependency rules.

## License

MIT
