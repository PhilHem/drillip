# How Drillip works

For deployment choices, shared histories, server-based commands, and access
boundaries, read [the operating model](operating-model.md).

**Ingestion:** Sentry SDKs POST error events. Drillip parses the envelope, extracts the exception or message, sanitizes fields, computes a fingerprint from the exception type and stack frame location or from the message text, and stores it in SQLite. Duplicate fingerprints increment the count.

**Notifications:** New errors and regressions (resolved errors that reappear) trigger email notifications. Emails include the exception, full stacktrace, request URL, user context, breadcrumbs, tags, and CLI commands to investigate further. Multiple errors within the digest window are batched into a single summary email. Failed sends are retried with exponential backoff. Silenced fingerprints are skipped.

**Lifecycle:** Errors can be resolved manually or after a period without occurrences.
A matching event after resolution reopens the error as a regression. Retention
removes old occurrences while preserving the grouped error and its total count.
See [How Drillip groups errors and tracks their lifecycle](error-lifecycle.md)
for grouping rules, states, and the relationship between resolution and retention.

**Correlation:** The `/api/0/correlate/<fp>/` endpoint assembles everything about an error in one response: stacktrace, breadcrumbs, user context, surrounding journalctl logs, system metrics from VictoriaMetrics, distributed trace spans from VictoriaTraces, and CPU profiles from Pyroscope. Optional telemetry sections are omitted when their integration is not configured or returns no data.

## Sharing a container

An application can run Drillip as a second process in its own container. The SDK
then sends events over loopback, and deployment needs one image and a persistent
database volume. A process manager handles restarts and lets the application
drain pending events before the tracker stops.

This couples deployment, resource limits, and container failure. Updating either
component replaces the combined container. Replicas have separate error histories
unless they send to a shared tracker instead. A process manager can recover from
individual process exits, but cannot preserve events still queued in memory when
the whole container is forcibly terminated.

Docker recommends separating concerns by service, while documenting process
managers for deliberate multi-process containers. This pattern trades independent
operation for a single deployment unit. See
[Docker's guidance](https://docs.docker.com/engine/containers/multi-service_container/),
the [Python tutorial](../tutorials/python-container.md), and the
[embedding guide](../how-to/embed-drillip.md).

## Next steps

For setup, use [Run Drillip](../how-to/run-drillip.md) and
[Send errors from your application](../how-to/send-errors.md).
[Email setup](../how-to/email-notifications.md) covers delivery checks;
[configuration](../reference/configuration.md) lists the available settings.
For the code's responsibilities and dependency rules, see the
[architecture explanation](architecture.md).
