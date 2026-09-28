# HTTP API reference

API responses use JSON, except for a successful health check, which returns
plain text `ok`. Handler errors use `{"error":"message"}` with an HTTP
error status. Paths below are relative to the running Drillip instance.

Server address settings are listed in the
[configuration reference](configuration.md#core).

## Ingest

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/1/store/` | Ingest a Sentry event (plain JSON) |
| `POST` | `/api/1/envelope/` | Ingest a Sentry envelope |

Both ingestion paths accept plain JSON or an event envelope and support
`Content-Encoding: gzip` and `br`. The request body is limited to 10 MiB;
decompressed input is read up to 10 MiB. A stored event returns
`{"id":"<fingerprint>"}`. Events without an exception or message are ignored
and return `{"id":"ok"}`.

Events are sanitized at ingest: oversized fields are truncated, invalid levels normalized, CRLF stripped from exception types.

## Query

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/0/top/` | Up to 25 errors sorted by occurrence count |
| `GET` | `/api/0/recent/?hours=1` | Errors first seen within the last N hours (max 8760) |
| `GET` | `/api/0/show/<fp>/` | Error detail with tag distribution |
| `GET` | `/api/0/trend/<fp>/` | Hourly occurrence histogram (24h) |
| `GET` | `/api/0/releases/<fp>/` | Which releases had this error |
| `GET` | `/api/0/stats/` | Total unique errors and occurrences |
| `GET` | `/api/0/correlate/<fp>/?nth=1` | Full context: stacktrace, logs, metrics, traces, profiles |

Query parameters for `top` and `recent`:

- `?level=error` — filter by severity
- `?tag=key=value` — filter by tag

For `recent`, `hours` defaults to `1`. Invalid or non-positive values use the
default; values above `8760` are capped at `8760`. For `correlate`, `nth`
defaults to `1` (the most recent occurrence); invalid or non-positive values
use that default.

`top`, `recent`, and `show` include a `state` field: `new`, `ongoing`, or
`resolved`. See the [lifecycle explanation](../explanation/error-lifecycle.md).
Correlation returns the available context; unconfigured or unavailable
integrations can leave sections absent.

## Actions

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/0/resolve/<fp>/` | Mark an error as resolved |
| `POST` | `/api/0/gc/?older_than=30d` | Delete occurrences older than duration |
| `POST` | `/api/0/silence/<fp>/?duration=24h&reason=...` | Silence notifications for an error |
| `DELETE` | `/api/0/silence/<fp>/` | Remove a silence |
| `GET` | `/api/0/silences/` | List active silences |
| `POST` | `/api/0/test-email/` | Send a test email to verify SMTP configuration |

`older_than` is required for garbage collection. It and the optional silence
`duration` accept a whole number followed by `h`, `d`, or `w`, such as `24h`,
`30d`, or `2w`. A silence without `duration` does not expire. The optional
`reason` is truncated to 500 bytes. URL-encode query parameter values.

The test-email endpoint returns `{"status":"sent","to":"<recipient>"}`
when SMTP accepts the message, HTTP `503` when notifications are not configured,
and HTTP `502` when sending fails. See the
[email setup guide](../how-to/email-notifications.md) for a complete check.

## Fingerprints

Fingerprint arguments accept 1–16 lowercase hexadecimal characters (`a-f`,
`0-9`). `show`, `trend`, `releases`, and `correlate` accept a prefix, for example
`/api/0/show/04827c/`. If several errors match, the lookup selects one match;
it does not reject an ambiguous prefix. `resolve` resolves all unresolved
errors with the prefix and returns the fingerprint of the first matched error.
It returns HTTP `404` when nothing matches or all matches are already resolved.

Use the full fingerprint for silence creation and removal: these operations
store and match the exact value, without expanding prefixes.

## Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/` or `/-/healthy` | Returns `ok` if the database is reachable |

A health check returns HTTP `200` with `ok` when the database is reachable,
or HTTP `503` with `{"error":"db unhealthy"}` when it is not.
