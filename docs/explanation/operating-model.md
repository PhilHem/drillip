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

## Reuse the deployment's access boundary

Drillip deliberately relies on the deployment's existing access controls instead
of maintaining another set of users, tokens, and roles. Applications and operators
already share a trusted host, container, or restricted network. Operators use
existing administrative access or authenticated SSH forwarding to reach the
tracker. This keeps deployment and operation small: there is no separate Drillip
authentication system to configure or maintain.

The supplied examples make that boundary concrete:

- The embedded tracker listens on container loopback. The application sends
  events locally; operators use `docker compose exec app drillip top` through
  their existing container-administration access.
- The systemd service listens on host loopback. Operators can work on that host
  or use [SSH port forwarding](../how-to/upgrade-cli.md#2-select-and-check-the-server)
  to run the CLI remotely through their existing SSH access.
- The separate Docker example publishes the tracker port on host loopback.
  Inside that container Drillip listens on all interfaces, so its container
  network also belongs to the chosen trust boundary.

Loopback is shared by local processes in the same host or container network
namespace; it does not distinguish administrators from other local users.
Every caller that can reach the endpoint has the same full API access, including
SDK senders: ingest, read, resolve, silence, delete occurrences, and test email.
Choose the host/container/network boundary for that shared level of trust.

The built-in endpoint uses HTTP. SSH forwarding provides the protected remote
path in this model. Deployments using HTTPS terminate TLS externally and retain
their chosen host, container, or network access boundary; transport encryption
alone does not grant or restrict API privileges.

Reports can contain user context, request data, tags, and breadcrumbs. Configure
your application's SDK to remove secrets and unwanted personal data before sending.
Drillip's ingest sanitization truncates and normalizes fields; it is not sensitive-
data redaction.

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
