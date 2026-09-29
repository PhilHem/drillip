# CLI reference

The same binary runs the HTTP server and provides commands for investigation
and maintenance. With no command, `drillip` starts the server, as does
`drillip serve`.

`resolve` and `health` call the configured server over HTTP. Normal resolution
therefore uses the server's notification policy and does not need database access
or SMTP configuration in the CLI process. Other investigation and maintenance
commands open SQLite directly and need access to the same database as the server.

## Invocation and global options

```text
drillip [--db <path>] [--addr <host:port>] [--offline] <command> [arguments]
```

| Option | Effect |
|---|---|
| `--db <path>` | Override `DRILLIP_DB` with a non-empty database path. |
| `--addr <host:port>` | Override `DRILLIP_ADDR` with a non-empty server listen address or HTTP command target. |
| `--offline` | With `resolve` only: update SQLite directly without notifications. |
| `--help` | Print global option help. |

Put global options before the command. Put command options before positional
arguments, including the fingerprint. For example:

```sh
drillip --db /data/errors.db correlate --nth 2 04827c
drillip --db /data/errors.db silence --reason "planned maintenance" 04827c0123456789 24h
```

Environment variables and defaults are listed in the
[configuration reference](configuration.md).

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
| `drillip top [--level <level>] [--tag <key=value>] [--limit <n>]` | List errors by total occurrence count; default limit `10`. |
| `drillip recent [--hours <n>] [--level <level>] [--tag <key=value>]` | List errors first seen within the last N hours; default `1`. |
| `drillip show <fingerprint>` | Show error details, stacktrace, and tag distribution. |
| `drillip trend <fingerprint>` | Show the hourly occurrence histogram for the last 24 hours. |
| `drillip correlate [--nth <n>] <fingerprint>` | Show error context for the Nth most recent occurrence; default `1`. |
| `drillip releases <fingerprint>` | Show retained occurrence counts by release. |
| `drillip stats` | Show the number of grouped errors and retained occurrences. |
| `drillip gc <duration>` | Delete occurrences older than the duration. |
| `drillip resolve <fingerprint>` | Resolve the uniquely identified error through the server. |
| `drillip silence [--reason <text>] <fingerprint> [duration]` | Silence notifications for the uniquely identified error, indefinitely if duration is omitted. |
| `drillip silences` | List active silences. |
| `drillip unsilence <fingerprint>` | Remove silences for the uniquely identified error. |
| `drillip health` | Call `/-/healthy` at the configured address; print `ok` on HTTP `200`, with a two-second request deadline. |

The health target uses loopback when the configured listen address is a wildcard
(`0.0.0.0`, `::`, or an empty host). The health command does not open SQLite.
The two-second deadline applies to builds containing this change; the example
image pinned to v0.3.14 has the command but no built-in deadline. Its startup
wrapper bounds each invocation separately.

`--level` filters by severity, for example `error` or `warning`. `--tag`
filters by one `key=value` pair. Durations for `gc` and `silence` are whole
numbers followed by `h`, `d`, or `w`, for example `24h`, `30d`, or `2w`.

`correlate` includes available data from the configured
[observability integrations](configuration.md#integrations-for-correlate).

## Resolve online or offline

Normally, use:

```sh
drillip --addr 127.0.0.1:8300 resolve 04827c
```

The command calls the same HTTP action as other API clients, with a ten-second
request deadline, and prints the resolved full fingerprint. Success confirms the
state change, not SMTP delivery. The server owns notification configuration and
retries. A failed request never falls back to a local database update. If the
connection fails after submission, inspect the error's state before retrying;
the server may already have applied the change.

For deliberate offline maintenance, use:

```sh
drillip --offline --db /data/errors.db resolve 04827c
```

This updates SQLite without contacting a server or sending notifications. There
is no notification queued for later delivery. `--offline` is rejected for other
commands, and passing `--db` to online `resolve` is rejected to prevent accidentally
resolving an error on a different server.

Earlier versions always resolved locally without email. To retain that behavior,
add `--offline` before the command. Upgrade the server together with the CLI to
use the same reference and notification contracts; older servers retain their
previous prefix semantics. The pinned v0.3.14 tutorial image retains its older
local resolution behavior until its Drillip version is upgraded.

## Fingerprints

Fingerprint arguments accept 1–16 lowercase hexadecimal characters (`a-f`,
`0-9`). All error operations accept a full fingerprint or a unique prefix.
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
