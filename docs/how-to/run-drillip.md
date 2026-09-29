# Run Drillip

Choose Docker, the Go binary, or systemd to run a Drillip server. For a guided
first run with disposable data, use the
[first-error tutorial](../tutorials/first-error.md).

## Docker

Run this command with Docker installed and running:

```bash
docker run -d \
  -v drillip-data:/data \
  -p 127.0.0.1:8300:8300 \
  -e DRILLIP_DB=/data/errors.db \
  -e DRILLIP_ADDR=0.0.0.0:8300 \
  ghcr.io/philhem/drillip:v0.3.14
```

The database persists in the `drillip-data` volume. The published port is
reachable on the host's loopback address.

To package Drillip inside an existing application's container, use
[Embed Drillip in an application container](embed-drillip.md). For a guided
example, follow [Run a Python service and Drillip in one container](../tutorials/python-container.md).

## Binary

With Go installed, run:

```bash
go install github.com/PhilHem/drillip@latest
drillip serve
```

The binary uses the configured database path and listen address. See the
[configuration reference](../reference/configuration.md) for those settings.

## systemd

Follow [Run Drillip as a systemd service](run-with-systemd.md) to install the
binary and unit, enable startup at boot, and check the running service.

## Check the server

Check the running server at `/-/healthy` on its configured address. A healthy
server returns `ok`. Then [connect your application](send-errors.md).
