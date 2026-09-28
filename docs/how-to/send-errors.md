# Send errors from your application

You need a running Drillip instance and a Sentry SDK installed in your
application. Set the SDK's DSN to the Drillip server, using the configuration
example for your language below.

Replace `127.0.0.1:8300` with the address reachable from your application.
Inside a separate container, `127.0.0.1` refers to that container. The DSN key
is ignored; the examples use `anykey`.

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
`/api/0/recent/` on the Drillip server to check for a newly grouped error.
Use `/api/0/top/` to find an existing group whose count increased instead.
See the [HTTP API reference](../reference/http-api.md#query) for the query
endpoints, and the [lifecycle explanation](../explanation/error-lifecycle.md)
for how reports are grouped.
