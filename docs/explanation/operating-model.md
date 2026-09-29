# One tracker, one shared error history

A Drillip instance owns one SQLite database and one notification and telemetry
configuration. Its SDK senders, API callers, and operators share that instance's
error history. This keeps a tracker for one service small and easy to deploy.

## Choose what belongs together

Use one instance for a service whose replicas should share grouping, resolution,
silences, and notifications. Use separate instances when applications need
independent histories, access boundaries, or notification/telemetry settings.

A Sentry DSN's key and project-number path do not create projects or enforce access
control. `DRILLIP_PROJECT` is an email label. Tags, release names and environments
help investigate events but do not isolate them; matching fingerprints can group
across those labels. See [grouping and lifecycle](error-lifecycle.md).

Embedding one tracker in each application container gives each replica its own
history and couples their upgrades and storage lifetimes. A shared tracker gives
replicas one history but introduces a separate service to operate. Both patterns
use one writer server per database; do not mount one SQLite file into several
Drillip servers. [Embedding](../how-to/embed-drillip.md) describes the first pattern.

## Keep the HTTP endpoint inside the intended trust boundary

Drillip does not authenticate requests or authorize operations. Anyone who can
reach its HTTP endpoint can read error details and available telemetry, submit
events, resolve or silence errors, delete occurrences, and trigger test email.
Reports can contain user context, request data, tags, and breadcrumbs. Configure
your application's SDK to remove secrets and unwanted personal data before sending.
Drillip's ingest sanitization truncates and normalizes fields; it is not sensitive-
data redaction.

The example host ports bind to loopback; the embedded tracker stays on container
loopback. Keep access limited to trusted applications and operators. Remote access
requires an access boundary outside Drillip, such as an SSH tunnel or an
appropriately restricted network/proxy. The built-in server listens over plain HTTP; HTTPS requires external TLS
termination. TLS protects transport; it does not by itself authorize callers. A proxy that only adds TLS is not access control.
Drillip is not a public multi-tenant Sentry replacement.

## Normal commands ask the server

In the current checkout, normal CLI commands use `DRILLIP_SERVER` or `--server`.
They do not need access to the database file. The running server provides the
same grouping and management operations to both CLI and HTTP clients and owns
SMTP and observability configuration. A shell's SMTP variables do not change the
server's notification policy.

The client checks server command-API compatibility before an operation. This
prevents older servers from silently ignoring newer operation parameters.
`health` checks availability separately, so a healthy older server can still be
incompatible with a newer command client.

Local `maintenance --db PATH` is an explicit alternative when an operator needs
an existing database directly. It performs no network requests, sends no email,
and returns stored correlation context without external telemetry. It does not
queue notifications for a later server restart. Use the normal server path when
you want normal notification and diagnostic behavior.

See [switch CLI access to the server](../how-to/upgrade-cli.md) for a tested
procedure and [CLI reference](../reference/cli.md) for exact option precedence.

## Stored events and delivery are different guarantees

A successful ingestion response containing a fingerprint confirms storage. An
ignored event instead returns `{"id":"ok"}` without storing an error. An SDK event ID is assigned
before delivery and does not prove the tracker received the event. A successful
resolution confirms a state change, not receipt of an email.

The supplied Python example's SDK queue and Drillip notification work are held
in memory. Graceful shutdown provides
an opportunity to finish them; forced termination can lose pending work. Persistent
storage preserves accepted error history across container replacement, but does
not make notification delivery durable. Choose a durable messaging system if
that is a required guarantee.
