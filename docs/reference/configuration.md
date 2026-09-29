# Configuration reference

Drillip reads configuration from environment variables when it starts. All
variables are optional. Unset or empty variables use the defaults below;
`—` means no value is set.

Server flags `serve --db` and `serve --listen` override `DRILLIP_DB` and
`DRILLIP_ADDR`. Client `--server` overrides `DRILLIP_SERVER`. For example:
`drillip serve --db /data/errors.db --listen 0.0.0.0:8300` and
`drillip --server http://127.0.0.1:8300 top`.

Legacy global `--addr` remains supported. Client target precedence is explicit
`--server` or legacy `--addr` (mutually exclusive), then `DRILLIP_SERVER`, then
legacy `DRILLIP_ADDR` converted to an HTTP URL, then `http://127.0.0.1:8300`.
Wildcard legacy addresses map to loopback. Server listen configuration ignores
`DRILLIP_SERVER`. Maintenance requires an explicit existing database path; see
[CLI modes and migration](cli.md#local-maintenance).

## Core

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_DB` | `errors.db` | Server SQLite database path |
| `DRILLIP_ADDR` | `127.0.0.1:8300` | Server listen address; explicit nonempty values also supply the legacy client target fallback |
| `DRILLIP_SERVER` | — | HTTP/HTTPS client target; default target and precedence above |
| `DRILLIP_PROJECT` | — | Project name shown in notifications |
| `DRILLIP_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |

Log levels are case-insensitive. `warning` is also accepted for `warn`.
Unrecognized values use `info`. Set `debug` to log every ingested event with
fingerprint, type, and new/regression status.

## Email notifications

Notifications are disabled when `DRILLIP_SMTP_HOST` or `DRILLIP_SMTP_TO` is empty.

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_SMTP_HOST` | — | SMTP server |
| `DRILLIP_SMTP_PORT` | `25` | SMTP port |
| `DRILLIP_SMTP_FROM` | — | Sender address |
| `DRILLIP_SMTP_TO` | — | Recipient address |
| `DRILLIP_SMTP_USER` | — | SMTP username (optional) |
| `DRILLIP_SMTP_PASS` | — | SMTP password (optional) |
| `DRILLIP_SMTP_SKIP_VERIFY` | `false` | Skip TLS certificate verification (`true` or `1`) |
| `DRILLIP_SMTP_COOLDOWN` | `60s` | Notification cooldown (`0` = no cooldown) |
| `DRILLIP_SMTP_DIGEST` | `5m` | Batch window for burst notifications (`0` = immediate) |

`DRILLIP_SMTP_COOLDOWN` and `DRILLIP_SMTP_DIGEST` accept durations such as
`60s`, `5m`, or `1h30m`, and the value `0`. They do not accept `d` or `w`.
An invalid value logs a warning and keeps the default.

In digest mode, distinct errors are buffered even during the cooldown. The batch
window is the longer of `DRILLIP_SMTP_DIGEST` and `DRILLIP_SMTP_COOLDOWN`, while
repeat notifications for the same fingerprint remain subject to the cooldown.
With digest disabled, the cooldown also throttles immediate sends globally.

`DRILLIP_SMTP_SKIP_VERIFY` is enabled only by the exact values `true` or `1`.
It disables TLS certificate verification, including when the SMTP server's
certificate authority is absent from the container's trust store.

Notifications are sent for:

- **New errors** — first time a fingerprint is seen
- **Regressions** — a resolved error reappears (amber-styled email with "was resolved for X" context)
- **Digests** — multiple new errors within the digest window are batched into one summary
- **Automatic resolutions** — the hourly maintenance task sends a summary of
  newly resolved errors that were previously marked as notified after successful
  SMTP delivery. Other stale errors are still resolved, but omitted from the email.
- **Manual resolutions** — the HTTP API and normal `drillip resolve` send a
  summary for the matched unresolved error, even if it was not previously
  notified. Explicit `drillip maintenance --db PATH resolve` does not send email.

Resolution summaries are sent directly, outside the new-error digest and
cooldown. Silencing a fingerprint suppresses new-error and regression emails;
it does not suppress a resolution summary.

Notification emails have at most three send attempts, with waits of 2 and
4 seconds before the retries. Test emails use one attempt and bypass digest
batching and cooldown.

## Lifecycle

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_RESOLVE_AFTER` | `24h` | Auto-resolve errors with no occurrences for this duration |
| `DRILLIP_RETAIN` | `90d` | Auto-delete occurrences older than this |

Both variables accept a whole number followed by `h`, `d`, or `w`, such as
`24h`, `90d`, or `2w`. A day is 24 hours and a week is 7 days. An invalid
value logs a warning and keeps the default. `DRILLIP_RETAIN=0h` disables
automatic occurrence deletion; bare `0` is invalid. `DRILLIP_RESOLVE_AFTER=0h`
does not disable auto-resolution.

Both tasks run hourly while the server is running. Expired silences are also
pruned in the same cycle.

## Integrations (for `correlate`)

| Variable | Default | Description |
|---|---|---|
| `DRILLIP_UNIT` | — | Systemd unit name for journalctl log correlation |
| `DRILLIP_VM_URL` | — | VictoriaMetrics base URL for metrics at time of error |
| `DRILLIP_VT_URL` | — | VictoriaTraces base URL for distributed trace spans |
| `DRILLIP_PYROSCOPE_URL` | — | Pyroscope base URL for CPU profiles |
| `DRILLIP_SERVICE` | — | Service name for Pyroscope queries |

These settings apply to both the `correlate` CLI command and the HTTP
correlation endpoint. Each integration is optional. Journal correlation needs
`journalctl` and access to the selected unit's logs. Trace correlation also
needs a trace ID on the occurrence. Profile correlation needs both
`DRILLIP_PYROSCOPE_URL` and `DRILLIP_SERVICE`.

Correlation enrichment has a shared five-second budget. Slow or unavailable
optional integrations leave stored error/occurrence context available; caller
cancellation stops integration HTTP requests and journal subprocesses. Journal
queries use absolute epoch timestamps, independent of the server timezone.

Metric queries must yield one series to produce a numeric value. Multiple series
are reported as `(ambiguous: multiple series)` instead of selecting an arbitrary
series; failures or exhausted budgets use `(error)` or `(timeout)`. The queries
have no per-service selector, so use a data source appropriate to this tracker's
scope. `cpu_seconds` is cumulative process CPU seconds, previously misleadingly
named `cpu_usage`; the query itself is unchanged. This is not a CPU utilization
percentage. Query-formula and selector design remain outside this change.
