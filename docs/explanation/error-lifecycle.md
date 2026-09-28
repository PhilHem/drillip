# How Drillip groups errors and tracks their lifecycle

Drillip groups repeated reports so that you can investigate one error while
tracking how often it occurs. Grouping, resolution, and retention answer
different questions: which reports belong together, whether an error is still
open, and how much occurrence history remains available.

## Events, occurrences, and errors

An **event** is a report sent by an application through the Sentry protocol.
When Drillip stores an event, it records an **occurrence** and creates or
updates the grouped **error** identified by its fingerprint.

The error holds its first-seen time, last-seen time, total count, and details
such as the exception and stacktrace. Each occurrence records a receipt time,
release, trace ID, and tags. Two stored events with the same fingerprint
produce one error with a count of two and two occurrence records.

Times reflect when Drillip receives the events. Repeated submissions count
again, even if they carry the same Sentry event ID.

## A fingerprint defines the group

Drillip sanitizes an event before computing its fingerprint. The fingerprint
is the first 16 hexadecimal characters of a SHA-256 hash. The input depends
on the kind of event:

- **Exception:** the type of the first exception, plus the filename, function,
  and line number from the last frame in that exception's stacktrace. Without
  a frame, only the exception type is used.
- **Message without an exception:** the message text, prefixed with `message:`.
  Drillip prefers the unformatted `logentry.message` template. Otherwise it
  uses `logentry.formatted` when present, or the event's `message` when there
  is no log entry. It strips recognized Loguru prefixes from the chosen text.

For example, `IOError: file not found: a.txt` and
`IOError: file not found: b.txt` belong to the same group when their selected
stack frame is the same. The exception value does not affect the fingerprint.
A change to that frame's line number can create a different group.

For message events, a template such as `checkout failed for order %s` keeps
reports together even when their formatted order numbers differ. Without a
template, changing the message text can create separate errors.

Release, environment, severity, tags, and user identity are not fingerprint
inputs. Reports from different releases or environments can therefore belong
to the same error in a Drillip database. Grouping identifies matching reports;
it does not prove that they share one root cause.

## State describes the grouped error

Drillip derives the displayed state when you query an error:

| State | Meaning |
|---|---|
| `new` | The error is unresolved and was first seen less than one hour ago. |
| `ongoing` | The error is unresolved and was first seen at least one hour ago. |
| `resolved` | The error has been marked as resolved, regardless of its age. |

An unresolved error becomes `ongoing` as it ages, even without another event.
Repeated occurrences update `last_seen` and increase the count, but preserve
`first_seen`. They do not restart the one-hour `new` period.

The displayed state `new` is also different from a new-error notification:
only the first stored event creates the error. Later events during that first
hour still show `new`, but do not each trigger a new-error notification.

## Resolution and regression

Manual resolution marks an error as resolved immediately. Automatic resolution
marks an unresolved error after it has gone without occurrences for the
configured interval. The server checks this hourly, so resolution does not
necessarily happen at the exact moment the interval ends.

Resolution records that the error is closed in Drillip. It neither changes
the application that reported it nor deletes the error's history.

When another event with the same fingerprint arrives after resolution,
Drillip clears the resolution, increases the existing count, and treats the
occurrence as a **regression**. The original `first_seen` is preserved.
Regression describes this transition; it is not a fourth displayed state.
The error returns to `new` or `ongoing` according to its original age.

For example, this sequence concerns one fingerprint:

| Time | What happens | Count | Displayed state |
|---|---|---|---|
| 09:00 | First event arrives. | 1 | `new` |
| 09:10 | A matching event arrives. | 2 | `new` |
| 10:01 | The error is queried, with no new event. | 2 | `ongoing` |
| 10:05 | The error is manually resolved. | 2 | `resolved` |
| 10:10 | A matching event arrives: a regression. | 3 | `ongoing` |

This example assumes automatic resolution has not yet applied. A regression
within the first hour would return to `new` instead.

New errors and regressions are eligible for email notifications when SMTP is
configured. Silences, cooldown, and digest batching affect notification
delivery; they do not prevent events from being stored or change their state.

## Retention removes occurrence history

Retention deletes old occurrence records. It leaves the grouped error, its
resolution status, first-seen and last-seen times, and total count in place.
Manual garbage collection has the same effect on the records it deletes.

As a result, an error can remain visible after all of its occurrences have
expired. Its total count can exceed the number of retained occurrences.
Trends, release history, and occurrence selection for correlation use the
remaining occurrence records, so they can contain less history than the
error's total count suggests. A later matching event still updates the
existing error; if it was resolved, the event is a regression.

The [lifecycle configuration reference](../reference/configuration.md#lifecycle)
lists the resolution and retention settings. To see grouping and manual
resolution in practice, follow
[Capture and resolve your first error](../tutorials/first-error.md).
