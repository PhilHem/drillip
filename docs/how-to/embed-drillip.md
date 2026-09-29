# Embed Drillip in an application container

Use this pattern when you want to deploy your service and its error tracker as
one container. Each container has its own Drillip database. For independent
upgrades or a shared tracker across replicas, use the
[separate-container setup](run-drillip.md#docker).

The [Python example](../../examples/python-container/Dockerfile) is a tested
template. To learn the pattern first, follow the
[tutorial](../tutorials/python-container.md). Its standard-library HTTP server
is a demonstration; keep your application's own server for a real deployment.

## Add the executable and runtime files

Keep your application's base image and copy `/drillip` from a pinned Drillip
release into `/usr/local/bin/drillip` with a multi-stage build. The example uses
`ghcr.io/philhem/drillip:v0.3.14` and builds for `linux/amd64`. Verify that the
binary matches your target architecture if you change the source image.

Install your image's CA certificates package if Drillip needs outbound TLS,
such as SMTP STARTTLS. Copying the executable does not copy its trust store.

For a Python image, install the example's pinned
[dependencies](../../examples/python-container/requirements.txt), then copy its
[Supervisor configuration](../../examples/python-container/supervisord.conf),
[readiness wrapper](../../examples/python-container/wait-for-drillip.py), and
[healthcheck](../../examples/python-container/healthcheck.py). Adapt these paths
and checks for your service. Other runtimes can use their existing process
manager with the same startup and shutdown behavior.

## Configure storage and connectivity

Set these environment variables in your image or deployment:

```text
DRILLIP_ADDR=127.0.0.1:8300
DRILLIP_DB=/var/lib/drillip/errors.db
SENTRY_DSN=http://anykey@127.0.0.1:8300/1
```

Mount a persistent volume at `/var/lib/drillip`. The example prepares that
directory for UID/GID 10001 and runs both processes as that user. For a bind mount,
make the host directory writable by the container's configured user before
starting it. Give each replica its own database volume; do not share one database
file between multiple Drillip servers.

Publish only your application's port. Use `docker exec <container> drillip top`
to query the embedded tracker. Set other `DRILLIP_*` variables as usual; see
[configuration](../reference/configuration.md) and
[email setup](email-notifications.md).

## Coordinate startup, recovery, and shutdown

Run Supervisor in the foreground as PID 1. Replace the example's application
command with your foreground server command. The readiness wrapper executes its
arguments directly, so it does not leave a shell between Supervisor and the
server. It waits up to ten seconds for Drillip's `/-/healthy` endpoint, then starts
the application even if the tracker is unavailable. Your SDK integration must
also tolerate unavailable error reporting.

Keep Drillip at priority 10 and the application at priority 20: Supervisor starts
lower priorities first and stops them last. With `autorestart=true`, it restarts
processes that exit after reaching `RUNNING`. Repeated failures during the
`startsecs` window exhaust `startretries` and leave a process in `FATAL`; there is
no unlimited retry in that state. See
[Supervisor's process settings](https://supervisord.org/configuration.html#program-x-section-settings).

Handle SIGTERM in your application: stop accepting work, finish active work
within a bounded time, and drain the SDK before exiting. The example calls
`sentry_sdk.get_client().close(timeout=5)` after stopping HTTP service. See
[Sentry's Python shutdown guidance](https://docs.sentry.io/platforms/python/configuration/draining/).
For a server with workers, ensure its signal handling drains each worker's SDK;
copying the example's main-process handler alone does not establish that behavior.

Keep enough time for both processes to stop. The example allows ten seconds for
Python and twenty for Drillip, and uses `docker stop --time 40`. Configure the
equivalent grace period in your deployment. Increase the application's budget
and the total grace period together if requests take longer. `stopasgroup` and
`killasgroup` ensure child processes receive the stop and, if necessary, kill
signals. Forced termination can still lose queued events.

## Check operation and diagnose a failed tracker

Use your application's readiness endpoint for container health. The example's
default healthcheck tests only the application: `healthy` means the application
can serve requests, even if error reporting is unavailable. An application outage
makes the container `unhealthy` independently of Drillip's state.

Check Drillip separately with the same probe's `drillip` target:

```bash
docker exec drillip-python python /app/healthcheck.py drillip
```

It queries Drillip's `/-/healthy` endpoint and prints `drillip: ok` with exit code
0, or `drillip: unavailable` with a nonzero exit code. Have your monitoring run
this diagnostic separately and alert on failure; application health alone does
not report a tracker outage. Supervisor also logs process failures.
Docker alone does not restart a container merely because its healthcheck fails.

Inspect both processes and their shared logs:

```bash
docker exec drillip-python supervisorctl -c /app/supervisord.conf status
docker logs drillip-python
docker inspect --format '{{.State.Health.Status}}' drillip-python
```

After fixing a runtime cause, such as volume permissions, start a stopped or
`FATAL` tracker with:

```bash
docker exec drillip-python supervisorctl -c /app/supervisord.conf start drillip
```

For image or environment changes, replace the container and retain its volume.
Confirm that the Drillip probe returns `drillip: ok`, send a test exception from
your application, and check `drillip top` for receipt. SDK event IDs alone do not prove
delivery. Events emitted while Drillip is down may be lost; this setup does not
provide a durable SDK queue.

From the repository root, test the unchanged example with:

```bash
docker build --platform linux/amd64 -t drillip-python-example examples/python-container
python3 examples/python-container/smoke_test.py
```

The test creates and removes its own container and volume. It checks capture,
process restarts, degraded operation, ordered shutdown, and persistence.
