# Run Drillip as a systemd service

Use the supplied unit to keep Drillip running and start it when the host boots.
These steps install a new instance on a Linux host with systemd, `sudo`,
`curl`, and the Go version declared in [go.mod](../../go.mod). Run the build
and installation commands from a Drillip checkout on that host.

The supplied unit listens on `127.0.0.1:8301`. That port must be free.

## 1. Install the binary and unit

Build the checkout and install the executable at the path used by the unit:

```sh
go build -o drillip .
sudo install -m 0755 drillip /usr/local/bin/drillip
sudo install -m 0644 deploy/drillip.service /etc/systemd/system/drillip.service
```

The [unit](../../deploy/drillip.service) uses a dynamic service user and
`StateDirectory=drillip`. systemd creates and manages its persistent state
directory; the database path seen by the service is
`/var/lib/drillip/errors.db`. You do not need to create a service account or
change ownership of that directory yourself.

## 2. Start the service and enable it at boot

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now drillip.service
sudo systemctl status --no-pager drillip.service
```

The status should show `active (running)`. The unit runs Drillip's health
command during startup and restarts the process if it exits unexpectedly.

## 3. Check the HTTP endpoint

```sh
curl --fail --silent --show-error http://127.0.0.1:8301/-/healthy
```

Expect `ok`. The service port is `8301`, which differs from the Docker
example's host port `8300`.

Use `http://anykey@127.0.0.1:8301/1` as the DSN for an application running on
the same host. See [Send errors](send-errors.md) for SDK configuration.

## Change configuration

Use an override to set environment variables without editing the supplied
unit:

```sh
sudo systemctl edit drillip.service
```

For example, to set the project name, save:

```ini
[Service]
Environment=DRILLIP_PROJECT=my-app
```

Apply the override and check the service again:

```sh
sudo systemctl daemon-reload
sudo systemctl restart drillip.service
sudo systemctl status --no-pager drillip.service
```

Repeat the health check. If you change `DRILLIP_ADDR`, use the new address in
the HTTP check and SDK DSN. Drillip's startup health command inherits the unit's
environment. The [configuration reference](../reference/configuration.md)
lists settings, and the [email guide](email-notifications.md) covers SMTP.

## If startup fails

Read the recent service logs:

```sh
sudo journalctl -u drillip.service -n 50 --no-pager
```

- For an executable error, check that `/usr/local/bin/drillip` exists, is
  executable, and was built for this host.
- For `address already in use`, choose a free listen address through the
  override, then restart and check that address.
- For a database permission error after customization, check that the database
  path is writable under the unit's `StateDirectory` and sandbox settings.
- If the process starts but the startup health check fails, inspect the logs
  and run the HTTP check against the configured address.

To stop the service and disable startup at boot:

```sh
sudo systemctl disable --now drillip.service
```

This leaves the database available for a later restart.
