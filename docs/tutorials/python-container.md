# Run a Python service and Drillip in one container

In this tutorial, you will build one image containing a small Python HTTP service
and Drillip, capture an exception, and keep the recorded error across container
replacement. Supervisor manages both processes. Only the Python port is published.

You need Docker with Compose v2, `curl`, a checkout of this repository, and a free
host port 18000. Start from the repository root. The pinned Drillip image used here
provides an amd64 binary, so Docker needs native amd64 support or emulation.
Use an unused Compose project named `drillip-python`: this tutorial deletes its
data at the end.

## Build and start the service

Build and start the [example](../../examples/python-container/Dockerfile) using
its [Compose file](../../examples/python-container/compose.yaml):

```bash
cd examples/python-container
docker compose up --build --wait --wait-timeout 60
```

The default image pins Drillip v0.3.17. To test the current checkout instead,
from the repository root run `docker build -t drillip:local .`, then use
`DRILLIP_IMAGE=drillip:local docker compose up --build --wait --wait-timeout 60`
in the example directory. The CLI and embedded server then use the same build.

Compose creates one container and a database volume, then waits for the service
to become healthy. Keep this directory for the remaining commands. Check its state:

```bash
docker compose ps
```

Expect one `app` service with status `healthy`. If startup fails, inspect
`docker compose logs` before continuing.

Container health describes the Python service. Confirm that the embedded tracker
is ready too:

```bash
docker compose exec app drillip health
```

Expect `ok`. If the probe reports an error, inspect the
container logs and repeat the probe before sending an event. Python remains
available during a tracker outage; error reporting is checked separately.

Check the Python service:

```bash
curl --silent --show-error http://127.0.0.1:18000/health
```

The response is `ok`. Inside the container, the SDK sends events to
`http://anykey@127.0.0.1:8300/1`. Drillip listens on the container's loopback
address; you do not need another container or a published tracker port.

## Capture an exception

Call the example's deliberately failing endpoint:

```bash
curl --silent --show-error --include http://127.0.0.1:18000/fail
```

Expect HTTP 500 and a JSON response containing `Example checkout failed` and an
[`event_id`](../reference/glossary.md#sdk-event-id). The SDK assigns that ID before
asynchronous delivery; check Drillip to confirm that it received the event:

```bash
docker compose exec app drillip top
```

Repeat the command after a moment if the list is still empty. You will see a
`RuntimeError` with the message `Example checkout failed` and a count of 1.
The [fingerprint](../reference/glossary.md#fingerprint) in this list identifies the
grouped error; it differs from the SDK event ID.

Call `/fail` again, then run `drillip top` again. The same error's count rises to 2.
Both events came from the same exception location, so Drillip grouped them.

## Replace the container without losing errors

Stop and remove the container:

```bash
docker compose down
```

Supervisor stops Python first. Python closes its Sentry client with up to five
seconds to send queued events while Drillip is still running. Supervisor then
stops Drillip. The Compose file sets a 40-second stop grace period for both
processes' stop budgets, so the stop command needs no timeout flag.
`down` preserves the database volume unless you request volume removal.

Start a replacement using the same volume:

```bash
docker compose up --wait --wait-timeout 60
```

Once health is `healthy`, check the errors:

```bash
docker compose exec app drillip top
```

The recorded error still has a count of 2. The SQLite database belongs to the
volume and survives replacement of the container.

## Clean up

This command removes the tutorial container and permanently deletes its error data:

```bash
docker compose down --volumes
```

To apply this pattern to your own application, use
[Embed Drillip in an application container](../how-to/embed-drillip.md).
For the deployment tradeoffs, read
[Sharing a container](../explanation/overview.md#sharing-a-container).
