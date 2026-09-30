<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/wordmark-dark.svg">
    <img src="docs/assets/brand/wordmark-light.svg" alt="Drillip" width="480">
  </picture>
</h1>

Lightweight, self-hosted error tracking. Drillip receives Sentry SDK error
[events](docs/reference/glossary.md#event), groups them in SQLite, and sends email
notifications for new errors and [regressions](docs/reference/glossary.md#regression).
Investigate errors through its CLI and HTTP API, with optional log, metric, trace,
and profile correlation.

One reason Drillip exists is to make Sentry SDK error reporting useful without
another user-management system to operate. For services already managed through
trusted host/container access or SSH, Drillip reuses that access boundary, with
no separate accounts, teams, or roles to maintain. See
[why Drillip keeps access management outside the tracker](docs/explanation/operating-model.md#why-drillip-keeps-access-management-outside-the-tracker).

The release examples pin v0.3.15, including its server-based CLI.
To upgrade an existing deployment, follow
[switch existing CLI commands to server access](docs/how-to/upgrade-cli.md).

## Quick start

Follow [Capture and resolve your first error](docs/tutorials/first-error.md)
to start Drillip in Docker, send an event, and resolve it.

For an existing application, [run Drillip](docs/how-to/run-drillip.md) and
[connect your Sentry SDK](docs/how-to/send-errors.md).

## Documentation

| Need | Documentation |
|---|---|
| Learn with a tutorial | [Capture and resolve your first error](docs/tutorials/first-error.md), [run Python and Drillip in one container](docs/tutorials/python-container.md) |
| Complete a task | [Run Drillip](docs/how-to/run-drillip.md), [switch CLI access](docs/how-to/upgrade-cli.md), [embed in an application container](docs/how-to/embed-drillip.md), [send errors](docs/how-to/send-errors.md), [set up email](docs/how-to/email-notifications.md) |
| Look up a term, setting, or interface | [Glossary](docs/reference/glossary.md), [Configuration](docs/reference/configuration.md), [HTTP API](docs/reference/http-api.md), [CLI](docs/reference/cli.md) |
| Understand the concepts | [Operating model and trust boundary](docs/explanation/operating-model.md), [how Drillip works](docs/explanation/overview.md), [error grouping and lifecycle](docs/explanation/error-lifecycle.md) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build and check commands and the
[architecture explanation](docs/explanation/architecture.md) for the source layout
and dependency rules.

## License

MIT
