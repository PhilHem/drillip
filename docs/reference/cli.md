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
drillip restore --input <backup-path> [--db <new-path>]
```

| Option | Effect |
|---|---|
| `--server <URL>` | Client target; HTTP or HTTPS, optionally with a path prefix. Overrides `DRILLIP_SERVER`. |
| `serve --listen <host:port>` | Server bind address; overrides `DRILLIP_ADDR`. |
| `serve --db <path>` | Server SQLite path; overrides `DRILLIP_DB`. |
| `restore --db <path>` | New local destination; required unless `DRILLIP_DB` supplies it. |
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
| `drillip health [--details]` | Check database access; print `ok`, or show persisted backup and restore times with `--details`. |
| `drillip backup --output <path>` | Save a complete database snapshot from the running server to a new local file. |
| `drillip restore --input <path> [--db <new-path>]` | Check a standalone Drillip backup and restore it to a new local database. |

Investigation and state commands have a ten-second deadline covering compatibility checking and
the operation. `health` has a two-second deadline. The simple probe does not need
the command API compatibility check; `health --details` checks `database_history`.
Failures never fall back to a database, follow redirects,
or automatically retry mutations. A connection failure after submission may mean
the server already changed state; inspect the state before retrying.

`--level` filters by severity; `--tag` accepts `key=value`. Durations for `gc` and
`silence` are whole numbers followed by `h`, `d`, or `w`. `correlate --nth` must
be positive. CLI `recent --hours` accepts 1–8760. Silence output reports the expiry
applied by the server, using the database's whole-second timestamp precision.

## Save a database backup

`backup --output <path>` requires a new local filename. It uses the selected
server and checks for the `database_backup` capability. An older server reports
an upgrade requirement before any backup download starts.

```console
$ drillip backup --output drillip-backup.db
saved drillip-backup.db
```

The operation has a two-minute deadline, including the compatibility check and
download. The server allows one backup at a time and checks the database snapshot
before sending it. The client streams the response to a temporary file in the
output directory. It publishes the complete file with permissions `0600`.
It does not replace an existing file, including one created during the download.
Failed downloads remove the temporary file and do not publish the destination.

The snapshot includes all stored database data. Deployment configuration is
separate. For restoration, see the [restore guide](../how-to/restore-backup.md).

## Restore a database backup

`restore` uses local files. It does not contact a server or read notification and
telemetry settings. `DRILLIP_SERVER` is ignored; an explicit global `--server`,
`--addr`, or `--db` is rejected. Set the destination with `restore --db` or
`DRILLIP_DB`. The Docker image sets `DRILLIP_DB=/data/errors.db`.

```console
$ drillip restore --input drillip-backup.db --db restored.db
restored restored.db
```

The input must be a standalone SQLite backup without WAL, shared-memory, or
journal files. Restore checks SQLite integrity, the required Drillip tables and
columns, and the backup format. Use a matching Drillip build. Older Drillip
backups without format metadata are accepted if their schema is compatible.

The command stages the data in the destination directory, records the new restore
time, and publishes a complete file with permissions `0600`. It preserves the
input and refuses an existing destination, symlink, or SQLite sidecar. A failure
before publication removes the staged files. The destination directory must exist.

New backups record the snapshot's data time. Restore preserves that time as the
restored data time and clears the source database's operation history. The new
restore time survives server restarts. Older backups have an unknown data time.
Restore does not start the server or change the deployment. See the
[restore guide](../how-to/restore-backup.md).

## Check health and database history

`health` calls `/-/healthy` and prints `ok` when the database is reachable.
`health --details` calls `/api/0/health/` after a capability check. Its two-second
deadline includes that check. Older servers report an upgrade requirement;
their simple health probe remains usable.

The times below are examples. Unknown times print `unknown`:

```console
$ drillip health --details
status: ok
last_backup_generated_at: 2026-09-30T12:00:08Z
last_restored_at: 2026-09-30T11:30:00Z
restored_snapshot_at: 2026-09-30T11:00:00Z
```

`last_backup_generated_at` records the last complete snapshot checked by this
database's server. It does not confirm a complete client download or storage
outside the server. `last_restored_at` records the last successful Drillip restore.
`restored_snapshot_at` is the data time of that restore's input. It is unknown for
older backups. New restores clear the source database's operation times; their
own times survive server restarts. Manual copying does not record a new restore.

These fields describe recorded operations. A missing or old backup does not
change health status. Neither health output nor a restore timestamp proves that
the database contains the history you expect; follow the restore guide's checks.

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
the server, search, filters, sort order, and page size.
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

## Upgrade from earlier CLI versions

Upgrade the server together with the CLI. Before each normal API operation, the
client checks `GET /api/0/capabilities/` for command API version 1. An older server
fails this check before any mutation; in particular it cannot silently ignore a
new silence expiry parameter. Direct HTTP clients may continue using the existing
API endpoints and duration parameters.

| Earlier invocation | Current invocation |
|---|---|
| `drillip --db /data/errors.db show 04827c` | `drillip --server http://127.0.0.1:8300 show 04827c` |
| `drillip maintenance --db /data/errors.db show 04827c` | `drillip --server http://127.0.0.1:8300 show 04827c` |
| `drillip --offline --db /data/errors.db resolve 04827c` | `drillip --server http://127.0.0.1:8300 resolve 04827c` |
| `drillip --addr 0.0.0.0:8300 --db /data/errors.db serve` | `drillip serve --listen 0.0.0.0:8300 --db /data/errors.db` |

`maintenance` and `--offline` fail with migration instructions. Global `--db`
remains a legacy server-start option and is rejected for client commands.
`DRILLIP_DB` does not select local command execution. To use a local database,
start `drillip serve --db PATH`, then use `drillip --server URL COMMAND`.
Resolution uses the server's notification configuration. Success confirms the
state change, not email delivery.

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
