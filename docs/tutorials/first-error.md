# Capture and resolve your first error

In this tutorial, you will start Drillip, send an error, see repeated events
grouped together, and mark the error as resolved.

You need a running Docker installation, `curl`, and `jq`. Use a Bash or Zsh
terminal on Linux, macOS, or WSL, and keep the same terminal open for all steps.
Port `18300` on your machine must be free.

## 1. Start Drillip

Run this container:

```sh
docker run --detach --rm \
  --name drillip-tutorial \
  --publish 127.0.0.1:18300:8300 \
  --mount type=tmpfs,destination=/data \
  --env DRILLIP_DB=/data/errors.db \
  --env DRILLIP_ADDR=0.0.0.0:8300 \
  ghcr.io/philhem/drillip:v0.3.16
```

Docker prints the container ID. This instance is reachable on your machine at
`http://127.0.0.1:18300`. Its database is temporary: stopping the container
deletes the tutorial data.

For the environment variables used here, see the
[configuration reference](../reference/configuration.md#core).

Check that Drillip is ready. The command retries while the server starts:

```sh
curl --fail --silent --show-error \
  --retry 10 --retry-connrefused --retry-delay 1 \
  http://127.0.0.1:18300/-/healthy
```

You should see `ok`.

## 2. Send an error

Create a sample event and send it to Drillip. Save the returned error identifier
in the `fingerprint` variable:

```sh
event='{"message":"Tutorial checkout failed","level":"error"}'
fingerprint=$(
  curl --fail --silent --show-error \
    --header 'Content-Type: application/json' \
    --data "$event" \
    http://127.0.0.1:18300/api/1/store/ |
    jq --exit-status --raw-output '.id'
)
printf '%s\n' "$fingerprint"
```

The output is a 16-character hexadecimal [fingerprint](../reference/glossary.md#fingerprint).
You will use it to look up this error in the next steps.

## 3. Read the stored error

Fetch the error and display its message, [occurrence](../reference/glossary.md#occurrence)
count, and state:

```sh
curl --fail --silent --show-error \
  "http://127.0.0.1:18300/api/0/show/$fingerprint/" |
  jq '{value, count, state}'
```

Immediately after sending the event, you should see:

```json
{
  "value": "Tutorial checkout failed",
  "count": 1,
  "state": "new"
}
```

## 4. Send the same error again

Send the same event a second time:

```sh
curl --fail --silent --show-error \
  --header 'Content-Type: application/json' \
  --data "$event" \
  http://127.0.0.1:18300/api/1/store/ |
  jq .
```

The response contains the same fingerprint in its `id` field. Read the error
again:

```sh
curl --fail --silent --show-error \
  "http://127.0.0.1:18300/api/0/show/$fingerprint/" |
  jq '{value, count, state}'
```

The `count` is now `2`. Both events belong to the same error.

## 5. Resolve the error

Mark the error as resolved:

```sh
curl --fail --silent --show-error --request POST \
  "http://127.0.0.1:18300/api/0/resolve/$fingerprint/" |
  jq .
```

The response contains the fingerprint and a `resolved_at` timestamp. Fetch
the error once more:

```sh
curl --fail --silent --show-error \
  "http://127.0.0.1:18300/api/0/show/$fingerprint/" |
  jq '{value, count, state}'
```

You should see:

```json
{
  "value": "Tutorial checkout failed",
  "count": 2,
  "state": "resolved"
}
```

The error remains available for inspection after you resolve it.

## 6. Clean up

Stop the tutorial instance:

```sh
docker stop drillip-tutorial
```

Docker prints `drillip-tutorial` and removes the container. The temporary
database is discarded. You can run the tutorial again from step 1.

You have captured an error, observed a repeated occurrence, and resolved it.
To send events from your own application, continue with
[Send errors from your application](../how-to/send-errors.md).

For the concepts behind these steps, read
[How Drillip groups errors and tracks their lifecycle](../explanation/error-lifecycle.md).
