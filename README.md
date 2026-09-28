<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/brand/wordmark-dark.svg">
    <img src="docs/assets/brand/wordmark-light.svg" alt="Drillip" width="480">
  </picture>
</h1>

Lightweight, self-hosted error tracking. Drillip receives Sentry SDK error
events, groups them in SQLite, and sends email notifications for new errors
and regressions. Investigate errors through its CLI and HTTP API, with
optional log, metric, trace, and profile correlation.

## Quick start

Follow [Capture and resolve your first error](docs/tutorials/first-error.md)
to start Drillip in Docker, send an event, and resolve it.

For an existing application, [run Drillip](docs/how-to/run-drillip.md) and
[connect your Sentry SDK](docs/how-to/send-errors.md).

## Documentation

| Need | Documentation |
|---|---|
| Learn with a tutorial | [Capture and resolve your first error](docs/tutorials/first-error.md) |
| Complete a task | [Run Drillip](docs/how-to/run-drillip.md), [send errors](docs/how-to/send-errors.md), [set up email](docs/how-to/email-notifications.md) |
| Look up a setting or interface | [Configuration](docs/reference/configuration.md), [HTTP API](docs/reference/http-api.md), [CLI](docs/reference/cli.md) |
| Understand the concepts | [How Drillip works](docs/explanation/overview.md), [error grouping and lifecycle](docs/explanation/error-lifecycle.md) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build and check commands and the
[architecture explanation](docs/explanation/architecture.md) for the source layout
and dependency rules.

## License

MIT
