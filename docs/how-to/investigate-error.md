# Find, investigate, and resolve an error

Use this guide to find an error group, inspect its details, and mark it resolved
after you fix the application.

Before you start, you need:

- The Drillip command-line client (`drillip`), version v0.3.16 or later, installed on your computer.
- A running Drillip server, version v0.3.15 or later.
- The server's HTTP or HTTPS URL.

To install the client, follow the [binary installation instructions](run-drillip.md#binary).

Set `DRILLIP_SERVER` to your server's URL. Replace the example URL below.
Use the same shell for the remaining commands:

```sh
export DRILLIP_SERVER=http://127.0.0.1:8300
drillip health
```

Expected output:

```text
ok
```

To update an older server, follow the [CLI upgrade guide](upgrade-cli.md).
For a remote server, you can use an [SSH tunnel](upgrade-cli.md#2-select-and-check-the-server).

## 1. Find the error group

If you know a tag that the application sends, use it to narrow the list.
Replace `service=checkout` with your actual tag:

```sh
drillip top --tag service=checkout --limit 50
```

If you only know the exception type or message, run `drillip top --limit 50`
and look for it in the `TYPE` and `VALUE` columns. Increase `--limit` when the
group is not in the first results. The list orders groups by their total count,
and long messages are shortened; inspect plausible matches with `show` below.

For an error that first appeared recently, you can instead use:

```sh
drillip recent --hours 24
```

`recent` selects groups **first seen** during that period. Another occurrence of
an older group, including a regression, does not bring it back into this list;
use `top` to find that group.

Copy the full 16-character value from the matching row's `FINGERPRINT` column
and save it. Replace the example below with the value you copied:

```sh
fingerprint=c2a8398a3347b02d
drillip show "$fingerprint"
```

Confirm the type, full message, and available stacktrace or tags match the
problem you are investigating. The SDK's `event_id` is not a group lookup key;
use the [Drillip fingerprint](../reference/glossary.md#fingerprint). Keep the
full value even if a shorter prefix happens to be unique today.

## 2. Choose the evidence you need

Use the commands that answer your investigation's questions:

| Question | Command | What to look for |
|---|---|---|
| Has the error spiked or stopped recently? | `drillip trend "$fingerprint"` | Hourly occurrence counts for the last 24 hours. |
| Which releases have reported it? | `drillip releases "$fingerprint"` | Retained occurrence counts and first/last receipt times for each release. |
| What context is available for the latest occurrence? | `drillip correlate "$fingerprint"` | Stored error details and available log, trace, metric, or profile results. |
| What about an earlier occurrence? | `drillip correlate --nth 2 "$fingerprint"` | Context for the second most recent retained occurrence; increase `--nth` as needed. |

External correlation sources must be configured on the server; see the
[integration settings](../reference/configuration.md#integrations-for-correlate).
Missing external results do not mean no error occurred. Retention can also
remove occurrence history while the group's total count remains; see
[what retention preserves](../explanation/error-lifecycle.md#retention-removes-occurrence-history).

## 3. Resolve after fixing the application

Apply and verify the application fix, then mark this group resolved:

```sh
drillip resolve "$fingerprint"
```

Expected output, using the example fingerprint:

```text
resolved c2a8398a3347b02d
```

The fingerprint in the output matches the group you selected. Check its state:

```sh
drillip top --limit 50
```

Its row now shows the state `resolved`; increase the list limit if needed.
Resolution can send an email according to the server's configuration.
It preserves the group's history and
does not itself fix the application. A later matching event reopens the group
as a [regression](../explanation/error-lifecycle.md#resolution-and-regression).

For all options and lookup errors, see the [CLI reference](../reference/cli.md).
