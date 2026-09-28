# CLI reference

The same binary runs the HTTP server and provides commands for investigation
and maintenance. With no command, `drillip` starts the server, as does
`drillip serve`.

Investigation and maintenance commands open the configured SQLite database
directly. They do not call the HTTP API. Run them with access to the same
database as the server. The `health` command instead calls the server over HTTP.

## Invocation and global options

```text
drillip [--db <path>] [--addr <host:port>] <command> [arguments]
```

| Option | Effect |
|---|---|
| `--db <path>` | Override `DRILLIP_DB` with a non-empty database path. |
| `--addr <host:port>` | Override `DRILLIP_ADDR` with a non-empty server listen address or health-check target. |
| `--help` | Print global option help. |

Put global options before the command. Put command options before positional
arguments, including the fingerprint. For example:

```sh
drillip --db /data/errors.db correlate --nth 2 04827c
drillip --db /data/errors.db silence --reason "planned maintenance" 04827c0123456789 24h
```

Environment variables and defaults are listed in the
[configuration reference](configuration.md).

## Commands

Angle brackets indicate required values; square brackets indicate optional
arguments. Do not type the brackets.

| Syntax | Effect |
|---|---|
| `drillip serve` | Start the HTTP server and background maintenance. |
| `drillip top [--level <level>] [--tag <key=value>] [--limit <n>]` | List errors by total occurrence count; default limit `10`. |
| `drillip recent [--hours <n>] [--level <level>] [--tag <key=value>]` | List errors first seen within the last N hours; default `1`. |
| `drillip show <fingerprint>` | Show error details, stacktrace, and tag distribution. |
| `drillip trend <fingerprint>` | Show the hourly occurrence histogram for the last 24 hours. |
| `drillip correlate [--nth <n>] <fingerprint>` | Show error context for the Nth most recent occurrence; default `1`. |
| `drillip releases <fingerprint>` | Show retained occurrence counts by release. |
| `drillip stats` | Show the number of grouped errors and retained occurrences. |
| `drillip gc <duration>` | Delete occurrences older than the duration. |
| `drillip resolve <fingerprint>` | Resolve all unresolved errors matching the prefix. |
| `drillip silence [--reason <text>] <fingerprint> [duration]` | Silence notifications for the exact fingerprint, indefinitely if duration is omitted. |
| `drillip silences` | List active silences. |
| `drillip unsilence <fingerprint>` | Remove silences for the exact fingerprint. |
| `drillip health` | Call `/-/healthy` at the configured address; print `ok` on HTTP `200`. |

`--level` filters by severity, for example `error` or `warning`. `--tag`
filters by one `key=value` pair. Durations for `gc` and `silence` are whole
numbers followed by `h`, `d`, or `w`, for example `24h`, `30d`, or `2w`.

`correlate` includes available data from the configured
[observability integrations](configuration.md#integrations-for-correlate).

## Fingerprints

Fingerprint arguments accept 1–16 lowercase hexadecimal characters (`a-f`,
`0-9`). `show`, `trend`, `correlate`, and `releases` accept a prefix such as
`04827c`. If several errors match, these lookups select one match rather than
rejecting the prefix. `resolve` applies to all unresolved matches.

Use the full fingerprint for `silence` and `unsilence`: these commands do not
expand prefixes. `show` prints the full fingerprint for a selected error.

For the meaning of counts and states, see the
[error lifecycle explanation](../explanation/error-lifecycle.md).
