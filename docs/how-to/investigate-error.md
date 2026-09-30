# Find, investigate, and resolve an error

Use this guide when an application has reported an error and you want to find
its group, examine the evidence, and mark it resolved after a fix. You need
access to the running tracker and the Go version in [go.mod](../../go.mod).

These steps use the CLI from this checkout, which prints full fingerprints in
lists and command hints. The v0.3.15 CLI still abbreviates those values. Build
the current CLI from the repository root and select your tracker's address:

```sh
go build -o drillip .
export DRILLIP_SERVER=http://127.0.0.1:8300
./drillip health
```

Expect `ok`. The server must support command API version 1 (v0.3.15 or later).
For an older server, follow [switch CLI access](upgrade-cli.md) to update the
server and client together. Updating the server from this checkout also makes
new notification emails use full fingerprints. For a remote tracker, use your
existing access path, such as the [SSH tunnel](upgrade-cli.md#2-select-and-check-the-server).

## 1. Find the error group

If you know a tag that the application sends, use it to narrow the list.
Replace `service=checkout` with your actual tag:

```sh
./drillip top --tag service=checkout --limit 50
```

If you only know the exception type or message, run `./drillip top --limit 50`
and look for it in the `TYPE` and `VALUE` columns. Increase `--limit` when the
group is not in the first results. The list orders groups by their total count,
and long messages are shortened; inspect plausible matches with `show` below.

For an error that first appeared recently, you can instead use:

```sh
./drillip recent --hours 24
```

`recent` selects groups **first seen** during that period. Another occurrence of
an older group, including a regression, does not bring it back into this list;
use `top` to find that group.

Copy the full 16-character value from the matching row's `FINGERPRINT` column
and save it. Replace the example below with the value you copied:

```sh
fingerprint=c2a8398a3347b02d
./drillip show "$fingerprint"
```

Confirm the type, full message, and available stacktrace or tags match the
problem you are investigating. The SDK's `event_id` is not a group lookup key;
use the [Drillip fingerprint](../reference/glossary.md#fingerprint). Keep the
full value even if a shorter prefix happens to be unique today.

## 2. Choose the evidence you need

Use the commands that answer your investigation's questions:

| Question | Command | What to look for |
|---|---|---|
| Has the error spiked or stopped recently? | `./drillip trend "$fingerprint"` | Hourly occurrence counts for the last 24 hours. |
| Which releases have reported it? | `./drillip releases "$fingerprint"` | Retained occurrence counts and first/last receipt times for each release. |
| What context is available for the latest occurrence? | `./drillip correlate "$fingerprint"` | Stored error details and available log, trace, metric, or profile results. |
| What about an earlier occurrence? | `./drillip correlate --nth 2 "$fingerprint"` | Context for the second most recent retained occurrence; increase `--nth` as needed. |

External correlation sources must be configured on the server; see the
[integration settings](../reference/configuration.md#integrations-for-correlate).
Missing external results do not mean no error occurred. Retention can also
remove occurrence history while the group's total count remains; see
[what retention preserves](../explanation/error-lifecycle.md#retention-removes-occurrence-history).

## 3. Resolve after fixing the application

Apply and verify the application fix, then mark this group resolved:

```sh
./drillip resolve "$fingerprint"
./drillip top --limit 50
```

Expect `resolved` followed by the full fingerprint. Its row now shows the state
`resolved`; increase the list limit if needed. Resolution can send an email
according to the server's configuration. It preserves the group's history and
does not itself fix the application. A later matching event reopens the group
as a [regression](../explanation/error-lifecycle.md#resolution-and-regression).

For all options and lookup errors, see the [CLI reference](../reference/cli.md).
