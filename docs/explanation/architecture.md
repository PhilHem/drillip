# Application architecture

Drillip separates application behavior from the protocols, storage, and
external services that deliver it. HTTP handlers and CLI commands use the
same application services. Those services coordinate domain models through
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
| Inbound adapters | Domain, inbound ports |
| Outbound adapters | Domain, outbound ports |
| Bootstrap | All application layers |

Calling an interface at runtime does not require importing its concrete
implementation. This lets a service use storage through an outbound port
while its imports remain independent of SQLite. Integration tests may connect
concrete adapters and services to check their behavior together.

## Bootstrap and the root executable

`bootstrap` reads configuration, constructs the concrete dependencies, and
connects them to services and adapters. It also runs the server lifecycle and
dispatches CLI commands. Normal `resolve` sends its request to the running
server so the server owns notification policy. Explicit `--offline resolve` uses
the local application service without a notifier. Other database commands retain
their direct local access. Constructing the application requires access to all
layers, so this wiring has its own package.

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
