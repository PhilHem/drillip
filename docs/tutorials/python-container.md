# Run a Python service and Drillip in one container

In this tutorial, you will build one image containing a small Python HTTP service
and Drillip, capture an exception, and keep the recorded error across container
replacement. Supervisor manages both processes. Only the Python port is published.

You need Docker, `curl`, a checkout of this repository, and a free host port 18000.
Run the commands from the repository root. The pinned Drillip image used here
provides an amd64 binary, so Docker needs native amd64 support or emulation.
Use unused container and volume names: this tutorial deletes its data at the end.

## Build and start the service

Build the [example](../../examples/python-container/Dockerfile):

```bash
docker build --platform linux/amd64 -t drillip-python-example examples/python-container
docker volume create drillip-python-data
docker run --detach --platform linux/amd64 --name drillip-python \
  --publish 127.0.0.1:18000:8000 \
  --mount type=volume,source=drillip-python-data,target=/var/lib/drillip \
  drillip-python-example
```

Check the container's health:

```bash
docker inspect --format '{{.State.Health.Status}}' drillip-python
```

Repeat until the output is `healthy`, normally within 15 seconds. If it becomes
`unhealthy`, inspect `docker logs drillip-python` before continuing.

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
`event_id`. The SDK assigns that ID before asynchronous delivery; check Drillip
to confirm that it received the event:

```bash
docker exec drillip-python drillip top
```

Repeat the command after a moment if the list is still empty. You will see a
`RuntimeError` with the message `Example checkout failed` and a count of 1.
The fingerprint in this list identifies the grouped error; it differs from the
SDK event ID.

Call `/fail` again, then run `drillip top` again. The same error's count rises to 2.
Both events came from the same exception location, so Drillip grouped them.

## Replace the container without losing errors

Stop and remove the container:

```bash
docker stop --time 40 drillip-python
docker rm drillip-python
```

Supervisor stops Python first. Python closes its Sentry client with up to five
seconds to send queued events while Drillip is still running. Supervisor then
stops Drillip. The 40-second Docker timeout accommodates both processes' stop
budgets.

Start a replacement using the same volume:

```bash
docker run --detach --platform linux/amd64 --name drillip-python \
  --publish 127.0.0.1:18000:8000 \
  --mount type=volume,source=drillip-python-data,target=/var/lib/drillip \
  drillip-python-example
docker inspect --format '{{.State.Health.Status}}' drillip-python
```

Once health is `healthy`, check the errors:

```bash
docker exec drillip-python drillip top
```

The recorded error still has a count of 2. The SQLite database belongs to the
volume and survives replacement of the container.

## Clean up

These commands remove the tutorial container and permanently delete its error data:

```bash
docker stop --time 40 drillip-python
docker rm drillip-python
docker volume rm drillip-python-data
```

To apply this pattern to your own application, use
[Embed Drillip in an application container](../how-to/embed-drillip.md).
For the deployment tradeoffs, read
[Sharing a container](../explanation/overview.md#sharing-a-container).
