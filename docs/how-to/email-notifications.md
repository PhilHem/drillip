# Set up and check email notifications

Use this guide to enable email notifications on an existing Drillip instance.
You need access to its configuration, an SMTP server reachable from Drillip,
a sender address, a recipient mailbox, and `curl`.

Have the SMTP host, port, and any required login credentials ready. Drillip
uses SMTP with STARTTLS when the server offers it. Use your provider's
STARTTLS endpoint, not an implicit-TLS endpoint such as port 465.

Docker images from v0.3.15 include a public CA bundle for TLS
certificate verification. The `v0.3.14` image used in the run guide does not
include it. For that image, mount a trusted PEM CA bundle read-only and set
`SSL_CERT_FILE` to its path inside the container. This also supports an SMTP
server that uses a private CA. Add these options before the image name:

```sh
--mount type=bind,source=/absolute/path/smtp-ca-bundle.pem,target=/certs/ca-bundle.pem,readonly \
--env SSL_CERT_FILE=/certs/ca-bundle.pem
```

Use the bundle supplied by your system or organization and keep it updated.
Leave `DRILLIP_SMTP_SKIP_VERIFY` unset to keep certificate verification enabled.

## 1. Configure SMTP

Add these variables to the environment of the Drillip server. Replace the
example values with your SMTP settings:

```dotenv
DRILLIP_PROJECT=my-app
DRILLIP_SMTP_HOST=smtp.example.com
DRILLIP_SMTP_PORT=587
DRILLIP_SMTP_FROM=drillip@example.com
DRILLIP_SMTP_TO=ops@example.com
DRILLIP_SMTP_USER=your-smtp-user
DRILLIP_SMTP_PASS=your-smtp-password
```

For a relay that does not require authentication, omit `DRILLIP_SMTP_USER`
and `DRILLIP_SMTP_PASS`. Keep credentials outside the source repository.

For all settings and their defaults, see the
[email configuration reference](../reference/configuration.md#email-notifications).

## 2. Apply the configuration

Drillip reads these variables when the server starts. Apply them through the
same tool that runs your instance:

- **Docker:** save the variables as `KEY=value` lines in an environment file.
  Add `--env-file /path/to/drillip-smtp.env` to your existing `docker run`
  command, before the image name, and recreate the container. Keep the same
  database volume, port mapping, and other settings. A container restart alone
  does not load changed environment variables.
- **systemd:** add the variables through an `EnvironmentFile=` in a service
  override. After changing the unit, run `sudo systemctl daemon-reload`, then
  `sudo systemctl restart drillip`.
- **Direct binary:** export the variables in the shell that starts Drillip,
  stop the existing process, and start `drillip serve` again with the same
  database path and other settings.

Check the server logs for `email notifications enabled`. For Docker, use
`docker logs <container-name>`; for systemd, use `journalctl -u drillip`.

## 3. Send a test email

Set the URL to your running instance, including its actual port. For the
[Docker setup](run-drillip.md#docker), use:

```sh
drillip_url=http://127.0.0.1:8300
```

Send a test email. This command prints the HTTP status, headers, and response
body, including any delivery error:

```sh
curl --silent --show-error --include --request POST \
  "$drillip_url/api/0/test-email/"
```

A successful request returns HTTP `200` and a JSON body with your configured
recipient:

```json
{"status":"sent","to":"ops@example.com"}
```

Open the recipient mailbox and look for `[drillip] test email from my-app`
(or the project name you configured). HTTP `200` means the SMTP server
accepted the message; check the mailbox to confirm delivery.

The test email is sent immediately. It bypasses the digest window and cooldown
and uses one send attempt. It does not create an error event. New-error and
regression notifications still follow the configured batching and cooldown.
For resolution summaries and other notification triggers, see the
[notification reference](../reference/configuration.md#email-notifications).

## If the test fails

Read the response body. When it includes a `hint`, follow that instruction. For
example, a rejected SMTP login returns HTTP `502` with this diagnosis:

```json
{
  "error": "The SMTP server rejected authentication.",
  "code": "smtp_auth_rejected",
  "hint": "Check DRILLIP_SMTP_USER, DRILLIP_SMTP_PASS, and the provider's authentication requirements."
}
```

`code` identifies the problem; `hint` gives the next check. Connection failures,
timeouts, certificate errors, and rejected addresses have their own diagnoses.
See the [test-email error reference](../reference/http-api.md#test-email-errors)
for the codes. An older server can return only `error`; use that text and the
server logs to identify the failed step.

| Result | Check |
|---|---|
| `curl` cannot connect to Drillip | Check the instance URL, published port, and whether the server is running. |
| HTTP `503`, `notifications not configured` | Set both `DRILLIP_SMTP_HOST` and `DRILLIP_SMTP_TO` in the server environment, then apply the configuration again. |
| HTTP `502` | Follow the response's `hint`. For additional details, check the Drillip server logs. |
| HTTP `200`, but no email arrives | Check spam folders, the recipient address, and the SMTP provider's delivery logs. |

After correcting the configuration, apply it again and repeat the test request.
