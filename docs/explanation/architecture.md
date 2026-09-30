# Application architecture

Drillip separates application behavior from the protocols, storage, and
external services that deliver it. HTTP handlers call inbound ports. Normal CLI
commands reach the server through the HTTP client. Explicit local maintenance
uses application services directly. Services coordinate domain models through
explicit port interfaces.

The directories identify each part's role:

```text
main.go                         process signals and exit status
internal/
  domain/                       events, error models, fingerprints, query results
  application/
    port/in/                    use-case interfaces (package inport)
    port/out/                   persistence, notification, and telemetry interfaces
    service/                    ingestion, error management, correlation, maintenance
  adapter/
    in/api/                     JSON HTTP API
    in/ingest/                  Sentry HTTP protocol
    in/cli/                     command parsing and output
    httpwire/                   shared command API representations
    out/httpclient/             remote application operations for the CLI
    out/sqlite/                 storage and migrations
    out/smtp/                   email delivery and formatting
    out/observability/          journalctl, VictoriaMetrics, VictoriaTraces, Pyroscope
  bootstrap/                    configuration, wiring, and server lifecycle
```

Tests live beside the code they exercise. Deployment files live in `deploy/`.

## Models, ports, and services

The domain holds shared models and rules, such as fingerprint calculation and
error-state derivation. It does not depend on application services or adapters.

Inbound ports describe what callers can ask the application to do. Outbound
ports describe the persistence, notification, and telemetry operations that
services need. Both port packages can use domain models, but neither depends
on services or concrete adapters. Keeping ports in named directories makes
the contracts visible separately from their implementations.

Application services implement the use cases and coordinate these contracts.
For example, the correlation service resolves a fingerprint prefix, selects
an occurrence, and collects available telemetry. Its inbound port returns
that combined result. The HTTP and CLI adapters then format it for their
respective interfaces.

Database backup has one inbound operation that returns a complete snapshot.
The SQLite adapter implements this operation directly. It owns consistent reads,
integrity checking, temporary files, and cleanup. Bootstrap connects it to the
HTTP handler. A forwarding service would add no policy or coordination here.
The HTTP client implements the same operation for the CLI, which publishes the
downloaded file. Neither caller needs SQLite handles or knowledge of WAL files.

## Adapters and dependency direction

Inbound adapters parse requests and format responses. They call inbound ports
without importing application services or concrete outbound adapters.
Outbound adapters implement access to SQLite, SMTP, and observability systems.

The permitted dependencies between layers are:

| Layer | May depend on |
|---|---|
| Domain | No other application layer |
| Inbound ports | Domain |
| Outbound ports | Domain |
| Application services | Domain, inbound ports, outbound ports |
| Inbound adapters | Domain, inbound ports, HTTP wire representations |
| Outbound adapters | Domain, outbound ports |
| HTTP client | Domain, inbound ports, HTTP wire representations |
| HTTP wire representations | Domain |
| Bootstrap | All application layers |

Calling an interface at runtime does not require importing its concrete
implementation. This lets a service use storage through an outbound port
while its imports remain independent of SQLite. Integration tests may connect
concrete adapters and services to check their behavior together.

## Bootstrap and the root executable

`bootstrap` reads configuration, constructs the concrete dependencies, and
connects them to services and adapters. It also runs the server lifecycle and
dispatches CLI commands. Parsing produces a validated invocation before any
backend is connected. Normal commands use one HTTP client implementing the
application operations; the server owns storage, notification, and telemetry
policy. Explicit maintenance opens the selected existing database and constructs
the application without a notifier or telemetry adapter.

The HTTP client accepts context on each operation and encapsulates target URLs,
query encoding, compatibility checks, deadlines, status errors, and response
validation. Shared wire representations keep client and handler field names
aligned. Its inbound-port dependency is deliberate: it is a remote implementation
of the operations consumed by the CLI, not a repository used by the server.
Application services reject already-cancelled contexts before storage access;
synchronous repository calls are not yet interruptible through these ports.

The root `main.go` handles process signals and exit status and delegates to
bootstrap. Keeping the executable at the repository root preserves the
install path `go install github.com/PhilHem/drillip@latest`. Application
packages live under `internal/` and cannot be imported by unrelated projects.

## Keeping the boundaries explicit

[The architecture test](../../internal/architecture_test.go) checks production
imports within `internal/` against the layer rules. It runs as part of the
Go tests and reports imports that cross a forbidden boundary.

See [Contributing](../../CONTRIBUTING.md) for build and validation commands.

### Complete investigation operations

Application queries accept a full fingerprint or unique prefix and return a
complete view with its canonical fingerprint, including an empty trend or release
history. The service owns reference resolution. CLI and HTTP adapters call one
operation and render the result; they never call `FindByPrefix` first. The
repository still exposes exact-fingerprint reads and reference lookup as internal
storage operations.
