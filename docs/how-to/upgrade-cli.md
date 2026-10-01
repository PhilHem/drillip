# Switch existing CLI commands to server access

Use this guide to move an existing deployment's operator commands to the current
checkout's server-based CLI. It covers direct binaries and the supplied systemd
unit. Command API version 1 is available from v0.3.15. This procedure requires a
checkout of that release or later, the Go version in
[go.mod](../../go.mod), `curl`, `jq`, and access to the host running the server.
Servers older than v0.3.15 do not support this command API.

Keep the current database location and deployment configuration. This change does
not move data or require a new database. Build both server and client from the
same checkout; installing only a new client leaves an older server incompatible.

## 1. Build and replace the executable

From the selected checkout:

```sh
go build -o drillip .
```

For a directly managed server, stop the old process gracefully, then start the new
binary with the same environment and database path. For example, using your actual
path and address:

```sh
./drillip serve --listen 127.0.0.1:8300 --db /data/errors.db
```

For an existing installation using the supplied systemd unit, replace only the
binary and keep your unit overrides and managed state directory:

```sh
sudo systemctl stop drillip.service
sudo install -m 0755 drillip /usr/local/bin/drillip
sudo systemctl start drillip.service
sudo systemctl status --no-pager drillip.service
```

Expect `active (running)`. For a new systemd installation, use the
[installation guide](run-with-systemd.md) instead. Give operator machines a binary
from the same checkout. For containers, build `docker build -t drillip:local .`
and replace the old container using its existing volume and environment; the
[embedded example](../tutorials/python-container.md#build-and-start-the-service)
has a checkout-image override.

## 2. Select and check the server

In a second shell, stay in the checkout containing the newly built `./drillip`
binary and set the actual server URL. The supplied
systemd unit uses port 8301; the Docker example uses host port 8300:

```sh
export DRILLIP_SERVER=http://127.0.0.1:8301
```

Check availability and compatibility separately:

```sh
curl --fail --silent --show-error "$DRILLIP_SERVER/-/healthy"
curl --fail --silent --show-error "$DRILLIP_SERVER/api/0/capabilities/" |
  jq --exit-status '.command_api == 1'
```

Expect `ok`, then `true`. If the second command fails, confirm that you restarted
the intended server with the newly built binary. Do not switch to another database
to work around a version mismatch. An explicit URL overrides legacy address
settings and avoids relying on another shell's environment.

For a tracker on another host, reuse your existing SSH access as the
[deployment boundary](../explanation/operating-model.md#reuse-the-deployments-access-boundary).
Keep the server on remote loopback and establish an SSH tunnel in a
separate terminal: `ssh -N -L 18301:127.0.0.1:8301 user@tracker-host`. Then use
`DRILLIP_SERVER=http://127.0.0.1:18301` locally. No public Drillip port is required.

## 3. Update scripts and verify an operation

Replace direct database commands, including `maintenance` and `--offline`, with
the server target:

```sh
# Earlier: drillip --db /data/errors.db top
# Earlier: drillip maintenance --db /data/errors.db top
./drillip --server "$DRILLIP_SERVER" top
```

If `top` reports no errors, that is a successful read check. To verify a write
without changing an existing error, create a uniquely named diagnostic event.
This deliberately adds an error to the selected tracker and can send new-error
and resolution notifications according to its configuration:

```sh
fingerprint=$(
  jq --null-input --arg message "CLI upgrade check $(date -u +%Y%m%dT%H%M%S)-$$" \
    '{message: $message, level: "error"}' |
    curl --fail --silent --show-error \
      --header 'Content-Type: application/json' --data-binary @- \
      "$DRILLIP_SERVER/api/1/store/" |
    jq --exit-status --raw-output '.id'
)
./drillip --server "$DRILLIP_SERVER" show "$fingerprint"
./drillip --server "$DRILLIP_SERVER" resolve "$fingerprint"
curl --fail --silent --show-error "$DRILLIP_SERVER/api/0/show/$fingerprint/" |
  jq --exit-status '.state == "resolved"'
```

Expect the resolved fingerprint, then `true`. Resolution can send an email
using the server's configuration. It confirms the state change, not mailbox
delivery. An already resolved error returns a failure without a new change.
Scripts must check exit status: zero means success; failures use stderr and a
nonzero status. See [CLI migration details](../reference/cli.md#upgrade-from-earlier-cli-versions).

Before replacing a Docker deployment, [back up its database volume and verify a
restore](restore-backup.md). Keep the original volume and configuration available
until the upgraded deployment is verified.
