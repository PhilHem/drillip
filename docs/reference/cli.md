# CLI reference

This describes the current checkout, including command API version 1 introduced
in v0.3.15. See
[the migration procedure](../how-to/upgrade-cli.md) to build a matching
client/server pair and verify the switch.

The same binary runs the server and provides investigation and management
commands. Normal commands use one HTTP or HTTPS server; they do not open a local
database or read local SMTP/observability settings. The server owns those policies.
With no command, `drillip` starts the server, as does `drillip serve`.

## Invocation and options

```text
drillip [--server <URL>] <command> [arguments]
drillip serve [--listen <host:port>] [--db <path>]
drillip maintenance --db <existing-path> <command> [arguments]
```

| Option | Effect |
|---|---|
| `--server <URL>` | Client target; HTTP or HTTPS, optionally with a path prefix. Overrides `DRILLIP_SERVER`. |
| `serve --listen <host:port>` | Server bind address; overrides `DRILLIP_ADDR`. |
| `serve --db <path>` | Server SQLite path; overrides `DRILLIP_DB`. |
| `maintenance --db <path>` | Explicit existing local database; no environment fallback. |
| `--help`, `<command> --help` | Print help without opening a database or contacting a server. |

Put `--server` before the command, and command options before positional
arguments. For example:

```sh
drillip --server http://127.0.0.1:8300 correlate --nth 2 04827c
drillip --server http://127.0.0.1:8300 silence --reason "planned maintenance" 04827c 24h
drillip serve --listen 127.0.0.1:8300 --db /data/errors.db
```

The default client target is `http://127.0.0.1:8300`. For compatibility, an explicit
legacy `--addr <host:port>` still selects an HTTP target and overrides environment
settings. `--server` and `--addr` together are rejected. If neither explicit flag
nor `DRILLIP_SERVER` is set, a nonempty `DRILLIP_ADDR` remains the legacy HTTP
target; wildcard bind addresses map to loopback. This preserves existing health
checks and prevents old configurations from silently selecting another server.
Prefer `--server` or `DRILLIP_SERVER` for new client configurations.

Environment variables are listed in the [configuration reference](configuration.md).

Successful commands, including help and empty result lists, exit with status 0.
Invalid arguments, failed lookups, and failed operations exit with status 1 and
report the error on stderr. Scripts should check the exit status rather than
parse the human-readable output. Earlier versions could print an error and still
exit successfully; automation relying on that behavior must handle failures now.

## Commands

Angle brackets indicate required values; square brackets indicate optional
arguments. Do not type the brackets.

| Syntax | Effect |
|---|---|
| `drillip serve` | Start the HTTP server and background maintenance. |
| `drillip list [options]` | Find error groups by last occurrence, text, or tags; see [list options](#find-error-groups). |
| `drillip top [--level <level>] [--tag <key=value>] [--limit <n>]` | List errors by total occurrence count; default limit `10`; must be positive. |
| `drillip recent [--hours <n>] [--level <level>] [--tag <key=value>]` | List errors first seen within the last N hours; default `1`, range `1–8760`. |
| `drillip show <fingerprint>` | Show error details, stacktrace, and tag distribution. |
| `drillip trend <fingerprint>` | Show the hourly occurrence histogram for the last 24 hours. |
| `drillip correlate [--nth <n>] <fingerprint>` | Show error context for the Nth most recent occurrence; default `1`, must be positive. |
| `drillip releases <fingerprint>` | Show retained occurrence counts by release. |
| `drillip stats` | Show the number of grouped errors and retained occurrences. |
| `drillip gc <duration>` | Delete occurrences older than the duration. |
| `drillip resolve <fingerprint>` | Resolve the uniquely identified error through the server. |
| `drillip silence [--reason <text>] <fingerprint> [duration]` | Silence notifications for the uniquely identified error, indefinitely if duration is omitted. |
| `drillip silences` | List active silences. |
| `drillip unsilence <fingerprint>` | Remove silences for the uniquely identified error. |
| `drillip health` | Call `/-/healthy` at the configured address; print `ok` on HTTP `200`, with a two-second request deadline. |

Normal commands have a ten-second deadline covering compatibility checking and
the operation. `health` has a two-second deadline and does not need the command
API compatibility check. Failures never fall back to a database, follow redirects,
or automatically retry mutations. A connection failure after submission may mean
the server already changed state; inspect the state before retrying.

`--level` filters by severity; `--tag` accepts `key=value`. Durations for `gc` and
`silence` are whole numbers followed by `h`, `d`, or `w`. `correlate --nth` must
be positive. CLI `recent --hours` accepts 1–8760. Silence output reports the expiry
applied by the server, using the database's whole-second timestamp precision.

## Find error groups

Use `list` to find a reported error. By default, it shows up to 50 groups,
most recently seen first, including resolved groups:

```sh
drillip list
drillip list --search "payment gateway"
drillip list --sort count
```

| Option | Effect |
|---|---|
| `--search <text>` | Match a literal substring in the complete stored exception type or message. ASCII letter case is ignored; other characters match exactly. `%` and `_` are literal characters. |
| `--sort last_seen` | Most recent occurrence first; the default. Equal timestamps are ordered by fingerprint ascending. |
| `--sort count` | Highest total occurrence count first, then last occurrence descending and fingerprint ascending. |
| `--level <level>` | Filter by severity. |
| `--tag <key=value>` | Filter by the group's stored tag, for example `service=checkout`. Tags come from the event that created the group. |
| `--limit <n>` | Page size, from `1` to `500`; default `50`. |
| `--offset <n>` | Skip this many matching groups; default `0`. Must not be negative. |

Search and filters apply on the server before the page is selected. Search uses
the group's stored type and message, including text beyond the list's shortened
`VALUE` column. It does not search a separate message history for each occurrence.
Use `show` with a result's full fingerprint to read its details.

When more matches exist, the output includes a next-page command that preserves
the server or maintenance database, search, filters, sort order, and page size.
Pages are separate queries: incoming errors can change their order between calls.
Repeat the search from the first page if new activity changes the results.

`top` remains the historical frequency ranking with a default limit of 10.
`recent` selects groups by their **first** occurrence, so an older group that
occurs again can appear first in `list` without appearing in `recent`.

`list` is available from v0.3.17. The server must advertise the `error_list`
feature in its
[capabilities response](http-api.md#command-api-compatibility-and-exact-times).
If it does not, `list` asks you to upgrade the server. Existing commands continue
to work with command API version 1 servers that do not offer this feature.

## Local maintenance

Use explicit maintenance when the server is unavailable and you deliberately
want direct access to its existing database:

```sh
drillip maintenance --db /data/errors.db show 04827c
drillip maintenance --db /data/errors.db resolve 04827c
```

Maintenance supports the same investigation and management commands, except
`health`. It never sends HTTP requests or email, and does not queue notifications
for later delivery. Correlation includes stored context only; remote telemetry
and journal lookups are disabled. Missing database files are rejected to avoid
silently creating the wrong database. A configured `DRILLIP_SERVER` or
`DRILLIP_DB` does not change an explicit maintenance invocation. Explicit global
server/database flags are rejected with maintenance.

Normal `resolve` confirms the state change, not SMTP delivery. The running server
owns notification configuration and delivery retries.

## Upgrade from earlier CLI versions

Upgrade the server together with the CLI. Before each normal API operation, the
client checks `GET /api/0/capabilities/` for command API version 1. An older server
fails this check before any mutation; in particular it cannot silently ignore a
new silence expiry parameter. Direct HTTP clients may continue using the existing
API endpoints and duration parameters.

| Earlier invocation | Current invocation |
|---|---|
| `drillip --db /data/errors.db show 04827c` | `drillip --server http://127.0.0.1:8300 show 04827c`, or explicit `maintenance --db /data/errors.db show 04827c` |
| `drillip --offline --db /data/errors.db resolve 04827c` | `drillip maintenance --db /data/errors.db resolve 04827c` |
| `drillip --addr 0.0.0.0:8300 --db /data/errors.db serve` | `drillip serve --listen 0.0.0.0:8300 --db /data/errors.db` |

`--offline` now fails with migration instructions. Global `--db` remains a legacy
server-start option and is rejected for normal commands. `DRILLIP_DB` does not
select local command execution. New normal commands require a running server;
use maintenance for deliberate local access.

## Fingerprints

[Fingerprint](glossary.md#fingerprint) arguments accept 1–16 lowercase hexadecimal
characters (`a-f`, `0-9`). All error operations accept a full fingerprint or a
unique [prefix](glossary.md#prefix).
An unknown reference fails; an ambiguous prefix fails and asks for a longer
fingerprint. `show` prints the full fingerprint, and state changes target exactly
one error. Silence creation also requires an existing error.

This changes earlier behavior: lookups no longer select an arbitrary match,
`resolve` no longer updates every match, and silence commands expand unique
prefixes. For a bulk operation, enumerate the intended full fingerprints and
invoke the command for each. Existing silences with no corresponding error can
still be removed by their exact stored fingerprint from `drillip silences`.

For the meaning of counts and states, see the
[error lifecycle explanation](../explanation/error-lifecycle.md).
