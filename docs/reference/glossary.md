# Glossary

Terms used in Drillip's commands, API, and documentation. Follow the links for
the related explanations and examples.

## Error (error group)

A group of matching [occurrences](#occurrence), identified by one
[fingerprint](#fingerprint), with a shared count and resolution state.
In Drillip's lists and management commands, an "error" means this group.
See [grouping and lifecycle](../explanation/error-lifecycle.md).

## Event

A report sent by an application using the Sentry event protocol.
When Drillip stores an event, it records an [occurrence](#occurrence) and creates
or updates its error group; some events are ignored rather than stored.
See [ingestion](http-api.md#ingest) for accepted payloads and responses.

## Fingerprint

The 16-character lowercase hexadecimal identifier that Drillip computes for an
error group from the event's [grouping inputs](../explanation/error-lifecycle.md#a-fingerprint-defines-the-group).
Use it to select the group in the CLI or HTTP API; it differs from the
[SDK event ID](#sdk-event-id).

## Occurrence

A stored record of one received event belonging to an error group.
Repeated submissions create separate occurrences, even with the same SDK event
ID; [retention](../explanation/error-lifecycle.md#retention-removes-occurrence-history)
can remove these records while preserving the group and its total count.

## Prefix

The leading characters of a fingerprint, usable as a shortcut only when they
identify exactly one error group in the selected tracker.
Ambiguous prefixes are rejected; see the [CLI reference](cli.md#fingerprints)
or [HTTP API reference](http-api.md#fingerprints) for the exact lookup rules.

## Regression

A matching event arriving after its error group was resolved, which reopens
that group and increases its count.
It describes a [lifecycle transition](../explanation/error-lifecycle.md#resolution-and-regression),
not an additional displayed state.

## SDK event ID

The `event_id` assigned by a Sentry SDK to an individual event before delivery.
It is neither confirmation that Drillip stored the event nor the fingerprint
used to look up its error group.
See the [Python tutorial](../tutorials/python-container.md#capture-an-exception)
for an example showing both identifiers.

## Tag

A name-value pair included in an [event](#event) to describe its context.
For example, an application can send `"tags": {"service": "checkout"}` to label
the reporting service. In the CLI, write this pair as `service=checkout`.
Tags are optional. `top --tag` and `recent --tag` filter by the tags stored when
the group was first created. `drillip show` displays them in its `Tags` section;
its `Tag Distribution` section summarizes tags across retained occurrences. See
[the investigation guide](../how-to/investigate-error.md#1-find-the-error-group)
for an example. The event's `environment` and `release` fields are separate
from its tags.
