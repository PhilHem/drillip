# Embed Drillip in an application container

Use this pattern when you want to deploy your service and its error tracker as
one container. Each container has its own Drillip database. For independent
upgrades or a shared tracker across trusted replicas of the same service, use the
[separate-container setup](run-drillip.md#docker). Shared trackers combine history
and configuration; see [the operating model](../explanation/operating-model.md).

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

## Keep deployment settings together

Copy the example's [Compose file](../../examples/python-container/compose.yaml)
beside your Dockerfile. It defines one application container with embedded
Drillip, a persistent named volume, the published application port, and a
40-second stop grace period. Adapt the build context and application port to your
image; keep the volume mounted at the configured database directory.

From the directory containing that file, run:

```bash
docker compose up --build --wait --wait-timeout 60
docker compose exec app drillip top
docker compose down
```

`down` preserves the database volume; `down --volumes` deletes it. The example
publishes port 18000 on host loopback. Set `APP_PORT` to choose another host port,
and use `--project-name` consistently if you need multiple independent deployments.
For example, this starts a separate project:

```bash
APP_PORT=18001 docker compose --project-name my-service up --build --wait --wait-timeout 60
```

The remaining commands assume the supplied Compose project and its `app` service.

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

Publish only your application's port. Use `docker compose exec app drillip top`
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
lower priorities first and stops them last. The example uses the following
recovery policy for each supervised process:

| Situation | Automatic behavior | Operator action |
|---|---|---|
| A process exits after reaching `RUNNING` | `autorestart=true` restarts it, including after exit code 0. There is no retry limit for exits from `RUNNING`. | Inspect logs if it keeps restarting. |
| A process cannot be spawned or exits before `startsecs=1` | Supervisor retries startup with increasing delays. When `startretries=3` is exhausted, the process enters `FATAL` and retries stop. | Fix the cause, then explicitly start the process or replace the container if its configuration changed. |
| Drillip is running but its HTTP endpoint is unavailable | Supervisor does not restart it just because its probe fails. Application health remains independent. | Alert on the separate Drillip probe and investigate logs. Restart Drillip after addressing the cause if needed. |
| An operator stops a process through `supervisorctl stop` | The process stays `STOPPED`; `autorestart` does not undo an explicit stop. | Use `supervisorctl start` when ready to resume it. |

`RUNNING` means the managed process has stayed up for `startsecs`, not that its
HTTP endpoint is ready. The application's readiness wrapper is part of that
process, so its waiting time also counts. Use the application healthcheck and
the separate Drillip probe to check availability. See Supervisor's
[process states](https://supervisord.org/subprocess.html#process-states) and
[process settings](https://supervisord.org/configuration.html#program-x-section-settings).

A child in `FATAL` does not stop Supervisor or exit the container. Check process
status and logs using the commands below; container restart policies are not a
substitute for this recovery policy.

Handle SIGTERM in your application: stop accepting work, finish active work
within a bounded time, and drain the SDK before exiting. The example calls
`sentry_sdk.get_client().close(timeout=5)` after stopping HTTP service. See
[Sentry's Python shutdown guidance](https://docs.sentry.io/platforms/python/configuration/draining/).
For a server with workers, ensure its signal handling drains each worker's SDK;
copying the example's main-process handler alone does not establish that behavior.

Keep enough time for both processes to stop. The example allows ten seconds for
Python and twenty for Drillip, and uses `docker stop --time 40`. Configure the
equivalent grace period in your deployment. The supplied Compose file already
sets [`stop_grace_period: 40s`](https://docs.docker.com/reference/compose-file/services/#stop_grace_period),
which applies to `docker compose stop` and `docker compose down`.
Increase the application's budget and the total grace period together if requests
take longer. `stopasgroup` and
`killasgroup` ensure child processes receive the stop and, if necessary, kill
signals. Forced termination can still lose queued events.

## Check operation and diagnose a failed tracker

Use your application's readiness endpoint for container health. The example's
default healthcheck tests only the application: `healthy` means the application
can serve requests, even if error reporting is unavailable. An application outage
makes the container `unhealthy` independently of Drillip's state.

Check Drillip separately with its built-in health command:

```bash
docker compose exec app drillip health
```

It queries Drillip's `/-/healthy` endpoint and prints `ok` with exit code
0, or an error on stderr with a nonzero exit code. Have your monitoring run
this diagnostic separately and alert on failure; application health alone does
not report a tracker outage. Supervisor also logs process failures.
Docker alone does not restart a container merely because its healthcheck fails.

Inspect both processes and their shared logs:

```bash
docker compose exec app supervisorctl -c /app/supervisord.conf status
docker compose logs
docker compose ps
```

After fixing a runtime cause, such as volume permissions, start a stopped or
`FATAL` tracker with:

```bash
docker compose exec app supervisorctl -c /app/supervisord.conf start drillip
```

For image or environment changes, replace the container and retain its volume.
Confirm that the Drillip probe returns `ok`, send a test exception from
your application, and check `drillip top` for receipt. SDK event IDs alone do not prove
delivery. Events emitted while Drillip is down may be lost; this setup does not
provide a durable SDK queue.

From the repository root, test the unchanged example with:

```bash
docker build --platform linux/amd64 -t drillip-python-example examples/python-container
python3 examples/python-container/smoke_test.py
python3 examples/python-container/compose_smoke_test.py
```

The tests create and remove their own containers and volumes. They check capture,
process restarts, degraded operation, ordered shutdown, persistence, and the
Compose deployment's configured stop grace period.
