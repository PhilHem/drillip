# Send errors from your application

You need a running Drillip instance and a Sentry SDK installed in your
application. Set the SDK's DSN to the Drillip server, using the configuration
example for your language below.

Replace `127.0.0.1:8300` with the address reachable from your application.
Inside a separate container, `127.0.0.1` refers to that container. The DSN key
is ignored; the examples use `anykey`. The project number also does not isolate
data. Every sender shares the instance's database and grouping rules. Use separate
instances for independent histories or access boundaries; see
[the operating model](../explanation/operating-model.md).

Drillip computes its own grouping fingerprint. SDK-supplied custom `fingerprint`
arrays, such as Python's `scope.fingerprint`, have no effect on grouping.
See [custom SDK fingerprints](../explanation/error-lifecycle.md#custom-sdk-fingerprints)
before relying on a Sentry grouping override.

```python
# Python
import sentry_sdk
sentry_sdk.init(
    dsn="http://anykey@127.0.0.1:8300/1",
    release="v1.2.0",
    environment="production",
)
```

```javascript
// JavaScript
Sentry.init({
  dsn: "http://anykey@127.0.0.1:8300/1",
  release: "1.2.0",
});
```

```go
// Go
sentry.Init(sentry.ClientOptions{
    Dsn:         "http://anykey@127.0.0.1:8300/1",
    Release:     "v1.2.0",
    Environment: "production",
})
```

These snippets configure an installed SDK; keep the SDK's imports and
initialization in your application's startup code.

Trigger an error that your SDK reports, then query
`/api/0/recent/` on the Drillip server to check for a new
[error group](../reference/glossary.md#error-error-group).
Use `/api/0/top/` to find an existing group whose count increased instead.
See the [HTTP API reference](../reference/http-api.md#query) for the query
endpoints, and the [lifecycle explanation](../explanation/error-lifecycle.md)
for how reports are grouped.
