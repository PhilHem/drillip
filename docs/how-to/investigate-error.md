# Find, investigate, and resolve an error

Use this guide to find an error group, inspect its details, and mark it resolved
after you fix the application.

Before you start, you need:

- The Drillip command-line client (`drillip`), version v0.3.16 or later, installed on your computer.
- A running Drillip server, version v0.3.15 or later.
- The server's HTTP or HTTPS URL.

To install the client, follow the [binary installation instructions](run-drillip.md#binary).

Set `DRILLIP_SERVER` to your server's URL. Replace the example URL below.
Run the commands in Bash or Zsh and keep the same shell open. In the terminal
examples, `$` marks a command. Lines without `$` show example output:

```console
$ export DRILLIP_SERVER=http://127.0.0.1:8300
$ drillip health
ok
```

To update an older server, follow the [CLI upgrade guide](upgrade-cli.md).
For a remote server, you can use an [SSH tunnel](upgrade-cli.md#2-select-and-check-the-server).

The examples below follow one checkout error. Your fingerprints, counts, times,
and error details will differ. `...` marks omitted output.

## 1. Find the error group

If you know a tag that the application sends, use it to narrow the list.
Replace `service=checkout` with your actual tag:

```console
$ drillip top --tag service=checkout --limit 50
FINGERPRINT       COUNT  LEVEL  STATE  TYPE          VALUE                                             LAST SEEN
────────────────  ─────  ─────  ─────  ────────────  ────────────────────────────────────────────────  ─────────
def8ed90f15e01eb  3      error  new    TimeoutError  Payment gateway did not respond within 3 seconds  1s ago

→ drillip show <fingerprint>
```

Look for the exception type or message in the `TYPE` and `VALUE` columns.
If you do not know a tag, or the filtered list has no matching group, run
`drillip top --limit 50` to search without a tag filter. The list orders groups
by their total count. Increase `--limit` if the group is not in the first results.
Long messages are shortened; use `show` below to read the full message.

For an error that first appeared recently, you can instead use:

```console
$ drillip recent --hours 24
New errors (last 24h):

FINGERPRINT       COUNT  LEVEL  STATE  TYPE          VALUE                                             FIRST SEEN
────────────────  ─────  ─────  ─────  ────────────  ────────────────────────────────────────────────  ──────────
b6276ba72bbc5084  1      error  new    ValueError    Invoice total must be positive                    0s ago
def8ed90f15e01eb  3      error  new    TimeoutError  Payment gateway did not respond within 3 seconds  4s ago

→ drillip show <fingerprint>
```

`recent` selects groups **first seen** during that period. Another occurrence of
an older group, including a regression, does not bring it back into this list;
use `top` to find that group.

Copy the full 16-character value from the matching row's `FINGERPRINT` column
and save it. Replace the example below with the value you copied:

```console
$ fingerprint=def8ed90f15e01eb
$ drillip show "$fingerprint"
── Error ──────────────────────────────────
Fingerprint: def8ed90f15e01eb
Level:       error
Type:        TimeoutError
Value:       Payment gateway did not respond within 3 seconds
Count:       3
...
── Stacktrace ─────────────────────────────
Traceback (most recent call last):
  File "app/checkout.py", line 48, in checkout
  File "app/payments.py", line 112, in charge_card
...
── Tags ───────────────────────────────────
  service: checkout
...
```

Confirm the type, full message, and available stacktrace or tags match the
problem you are investigating. The SDK's `event_id` is not a group lookup key;
use the [Drillip fingerprint](../reference/glossary.md#fingerprint). Keep the
full value even if a shorter prefix happens to be unique today.

## 2. Choose the evidence you need

Use the commands that answer your investigation's questions:

| Question | Command | What to look for |
|---|---|---|
| Has the error spiked or stopped recently? | `drillip trend "$fingerprint"` | Stored occurrence counts by UTC hour for the last 24 hours. Hours without stored occurrences are omitted. |
| Which releases have reported it? | `drillip releases "$fingerprint"` | Stored occurrence counts and first/last receipt times for each release. |
| What context is available for the latest occurrence? | `drillip correlate "$fingerprint"` | The selected occurrence's time, stored group details, and available logs, traces, metrics, or profiles. |
| What context is available for an earlier occurrence? | `drillip correlate --nth 2 "$fingerprint"` | The `Occurrence: #2` line confirms selection of the second most recent stored occurrence. |

For example, check which occurrence `correlate --nth 2` selects:

```console
$ drillip correlate --nth 2 "$fingerprint"
── Error ──────────────────────────────────
Type:        TimeoutError
Value:       Payment gateway did not respond within 3 seconds
Fingerprint: def8ed90f15e01eb
Occurrence:  #2 at 2026-09-30T08:57:36Z (3s ago)
...
```

Increase `--nth` to select an older stored occurrence. Check the `Occurrence`
line to confirm the selection. If this line is absent, the output only confirms
the group details. Try a smaller `--nth` value.

The stored message, stacktrace, and breadcrumbs describe the group. `--nth`
selects the occurrence whose time and trace ID are used for external lookups.
External correlation sources must be configured on the server; see the
[integration settings](../reference/configuration.md#integrations-for-correlate).

An external source can be unconfigured, unavailable, or return no matching data.
Missing external results do not mean no error occurred. Retention can also
remove occurrence history while the group's total count remains; see
[what retention preserves](../explanation/error-lifecycle.md#retention-removes-occurrence-history).

## 3. Resolve after fixing the application

Apply and verify the application fix, then mark this group resolved:

```console
$ drillip resolve "$fingerprint"
resolved def8ed90f15e01eb
```

The output above uses the example fingerprint. Your output shows the fingerprint
of the group you selected. Check its state:

```console
$ drillip top --limit 50
FINGERPRINT       COUNT  LEVEL  STATE     TYPE          VALUE                                             LAST SEEN
────────────────  ─────  ─────  ────────  ────────────  ────────────────────────────────────────────────  ─────────
def8ed90f15e01eb  3      error  resolved  TimeoutError  Payment gateway did not respond within 3 seconds  2s ago
b6276ba72bbc5084  1      error  new       ValueError    Invoice total must be positive                    1s ago

→ drillip show <fingerprint>
```

Find the row with your fingerprint and check its `STATE` column. Increase
`--limit` if the row is missing from the list.
Resolution can send an email according to the server's configuration.
It preserves the group's history and does not itself fix the application.
A later matching event reopens the group
as a [regression](../explanation/error-lifecycle.md#resolution-and-regression).

For all options and lookup errors, see the [CLI reference](../reference/cli.md).
