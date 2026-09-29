# HTTP API reference

This describes the current checkout. Command API compatibility and absolute-time
parameters require the matching server build; the pinned v0.3.14 examples use the
older relative-parameter API.

The API deliberately has no separate credentials or roles. Access control belongs
to the [deployment boundary](../explanation/operating-model.md#reuse-the-deployments-access-boundary),
such as existing host/container administration or SSH forwarding. All callers
that can reach the endpoint have the same full API access.

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
| `GET` | `/api/0/top/` | Errors sorted by occurrence count; `limit` defaults to 25 |
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

`older_than` or absolute `before` is required for garbage collection. `older_than` and the optional silence
`duration` accept a whole number followed by `h`, `d`, or `w`, such as `24h`,
`30d`, or `2w`. A silence without `duration` or `expires_at` does not expire. The optional
`reason` is truncated to 500 bytes. URL-encode query parameter values.

The test-email endpoint returns `{"status":"sent","to":"<recipient>"}`
when SMTP accepts the message, HTTP `503` when notifications are not configured,
and HTTP `502` when sending fails. See the
[email setup guide](../how-to/email-notifications.md) for a complete check.

## Command API compatibility and exact times

`GET /api/0/capabilities/` returns `{"command_api":1}`. Version 1 promises:

- `top?limit=N` accepts a positive limit; list entries include `first_seen`.
- `recent?since=TIMESTAMP` and `trend/<fp>/?since=TIMESTAMP` accept absolute start times.
- `gc?before=TIMESTAMP` accepts an absolute deletion cutoff.
- `silence/<fp>/?expires_at=TIMESTAMP` accepts an absolute expiry and returns
  the actual applied `expires_at` (UTC, whole-second storage precision).

Absolute timestamps use RFC3339, including optional fractional seconds and offsets;
URL-encode them. They reach application operations without conversion to rounded
hour/day durations. Existing SQLite comparisons and stored times use whole seconds.
Using both `since` and `hours`, `before` and `older_than`, or `expires_at` and
`duration` fails with HTTP 400. Malformed absolute timestamps also fail with 400.
Existing relative parameters retain their behavior.

The command client checks this contract before each operation and rejects older
servers before mutating state. Health probes remain independently usable.

## Fingerprints

Fingerprint arguments accept 1–16 lowercase hexadecimal characters (`a-f`,
`0-9`). All error operations accept a full fingerprint or a unique prefix.
Unknown references return HTTP `404`; ambiguous prefixes return HTTP `409` without
changing any error. Use a longer fingerprint to select exactly one error.
`resolve` updates only that error and returns its full fingerprint; an already
resolved error returns `404`. Silence creation also requires an existing error,
and silence creation/removal responses report the full fingerprint.

Earlier versions selected an arbitrary lookup match, resolved all prefix matches,
and treated silence arguments as exact strings. Clients relying on bulk resolution
must enumerate the intended full fingerprints and send one request per error.
Existing silences without a corresponding error can still be deleted by their
exact stored fingerprint from the silences list.

## Health

| Method | Path | Description |
|---|---|---|
| `GET` | `/` or `/-/healthy` | Returns `ok` if the database is reachable |

A health check returns HTTP `200` with `ok` when the database is reachable,
or HTTP `503` with `{"error":"db unhealthy"}` when it is not.

## Response fields

Successful calls to the listed endpoints return HTTP `200`, including actions
and ingestion. Optional fields can be absent. Stored JSON fields such as
`stacktrace` can also contain `null`; they are JSON values, not encoded strings.
Unless noted otherwise, timestamps generated by Drillip are UTC RFC3339 strings.

### Error summaries and detail

`top` and `recent` return arrays of error summaries (`[]` when empty).
`show` returns one detail object with the same summary fields plus the detail
fields below.

| Summary field | JSON type | Meaning |
|---|---|---|
| `fingerprint` | string | Full error fingerprint. |
| `count` | integer | Total recorded occurrences, including those later removed by retention. |
| `level` | string | Stored severity. |
| `type` | string | Exception type, or `message` for a message event. |
| `value` | string | Stored exception value or message text. |
| `last_seen` | string | Time of the latest stored occurrence. |
| `state` | string | `new`, `ongoing`, or `resolved`. |
| `resolved_at` | string, optional | Resolution time; absent while unresolved. |

| Additional detail field | JSON type | Meaning |
|---|---|---|
| `first_seen` | string | Time the error was first recorded. |
| `release`, `environment`, `platform` | string, optional | Stored event metadata. |
| `stacktrace` | object or null, optional | Stored Sentry stacktrace. |
| `breadcrumbs` | array or null, optional | Stored Sentry breadcrumb entries. |
| `user` | JSON value, optional | Stored Sentry user context. |
| `tags` | JSON value, optional | Stored event tags. |
| `tag_distribution` | object, optional | Tag keys mapped to an object with a `values` array. Each value has `value` (string), `count` (integer), and `percent` (integer). |

For example, a summary array can contain:

```json
[
  {
    "fingerprint": "57fe09195ae994ab",
    "count": 2,
    "level": "error",
    "type": "message",
    "value": "Tutorial checkout failed",
    "last_seen": "2026-09-28T12:00:00Z",
    "state": "new"
  }
]
```

### Trends, releases, and statistics

| Endpoint | Response fields |
|---|---|
| `trend` | `fingerprint` (string), `buckets` (array). Each bucket has `hour` (UTC string in `YYYY-MM-DD HH:00` format) and `count` (integer). Empty buckets are not filled in. |
| `releases` | `fingerprint` (string), `releases` (array). Each entry has `release` (string), `count` (integer), `first_seen` and `last_seen` (strings). An unlabelled release is an empty string. |
| `stats` | `unique_errors` and `total_occurrences` (integers), plus optional `first_seen` and `last_seen` (strings). `total_occurrences` counts retained records; the time range comes from grouped errors. |

`buckets` and `releases` are `[]` when no retained records match. Both describe
retained occurrence history, which can be shorter than an error's total count.

### Correlation

`correlate` always returns `fingerprint`, `type`, and `value` as strings when
the error is found. Additional fields depend on the stored data and available
integrations:

| Optional field | JSON shape |
|---|---|
| `occurrence` | Object with `nth` (integer), `timestamp` (string), and optional `trace_id` (string). |
| `stacktrace`, `breadcrumbs`, `user` | Stored JSON values, as in error detail. |
| `logs` | Array of objects with `timestamp`, `message`, and optional `priority` (strings). The timestamp is journalctl's raw microseconds-since-epoch value. |
| `trace` | Object with `service_name` (string) and `spans` (array, or `null` when empty). Each span has `operation_name` and `duration` (strings, for example `"1.5ms"`). |
| `metrics` | Object mapping metric names to string values. An individual failed query can have the value `"(error)"`. |
| `profile` | Array of objects with `function` (string). |

A missing occurrence or unavailable integration can still produce HTTP `200`
with partial context. There is no response field that lists integration errors.

### Action responses

| Endpoint | Response fields |
|---|---|
| `POST resolve` | `fingerprint` (string, matched error) and `resolved_at` (string). |
| `POST gc` | `deleted` (integer, number of removed occurrences) and `threshold` (string). |
| `POST silence` | `fingerprint` (string), `status: "silenced"`, and optional `expires_at` (string). |
| `DELETE silence` | `fingerprint` (string) and `status: "unsilenced"`. |
| `GET silences` | Array of objects with `fingerprint` and `created_at` (strings), plus optional `expires_at` and `reason` (strings). With no active silences, the response is `null`. |
| `POST test-email` | `status: "sent"` and `to` (string, configured recipient). |

Creating a silence requires an existing error. Removing a silence for a known
error returns success even if it was not silenced.

## Error status codes

The following statuses describe the handlers for the paths listed above.
Routing redirects and responses from a reverse proxy are outside this table.
Error bodies have the form `{"error":"message"}`.

| Endpoint | Error status codes |
|---|---|
| Ingestion (`store`, `envelope`) | `400` for a body read or payload parse error; `405` for a method other than POST; `500` for a storage failure. |
| `top`, `recent`, `stats`, `silences` | `405` for a method other than GET; `500` for a query failure. |
| `show` | `400` for an invalid fingerprint; `404` for an unknown reference; `409` for ambiguity; `500` for retrieval failure; `405` for a method other than GET. |
| `trend`, `releases` | `400` for an invalid fingerprint; `404` for an unknown reference; `409` for ambiguity; `405` for a method other than GET; `500` for a history query failure. |
| `correlate` | `400` for an invalid fingerprint; `404` for an unknown reference; `409` for ambiguity; `500` for retrieval failure; `405` for a method other than GET. |
| `resolve` | `400` for an invalid fingerprint; `404` for an unknown or already resolved error; `409` for ambiguity; `405` for a method other than POST; `500` for a storage failure. |
| `gc` | `400` for a missing or invalid `older_than`; `405` for a method other than POST; `500` for a deletion failure. |
| `silence` | `400` for an invalid fingerprint or duration; `404` for an unknown reference; `409` for ambiguity; `405` for a method other than POST or DELETE; `500` for a storage failure. |
| `test-email` | `405` for a method other than POST; `502` for an SMTP send failure; `503` when notifications are not configured. |
| Health | `503` when the database check fails. The health handler does not restrict the HTTP method. |

Some invalid query values are accepted with defaults instead of an error:
`hours` and `nth` follow the rules under [Query](#query), and a malformed
`tag` filter is ignored. Unknown query parameters are ignored.
