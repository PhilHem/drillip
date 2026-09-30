# Restore Drillip data in Docker

Use an existing database backup to recover Drillip data or check that the backup
can be restored. You do not need access to the source server. This procedure
keeps the backup and the original data volume.

To create a backup first, use [Back up Drillip data](backup-restore.md).

## Before you start

You need Docker, a Drillip client, and a Drillip image from this checkout.
Use an image from the same build as the backup's source server. This example uses
`drillip:local` from the
[checkout build instructions](upgrade-cli.md#1-build-and-replace-the-executable).
Keep the deployment configuration saved with your backup available for recovery.
Host port `18301` must be free.

Run the commands in the same Bash session. In terminal examples, `$` marks a
command. Lines without `$` show example output.

## 1. Restore the backup to a new volume

Set `backup_file` to your backup's absolute path and `restore_image` to the
matching image. Choose unused volume and container names:

```bash
set -euo pipefail
backup_file="$PWD/drillip-backup.db"
restore_image=drillip:local
restored_volume=drillip-restored
restored_container=drillip-restore-check
if docker volume inspect "$restored_volume" >/dev/null 2>&1; then
  printf 'Choose a new destination volume name.\n' >&2
  exit 1
fi
docker volume create "$restored_volume"
```

Restore with the Drillip image. The backup mount is read-only:

```bash
docker run --rm \
  --mount "type=bind,src=$backup_file,dst=/backup.db,readonly" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  "$restore_image" restore --input /backup.db
```

The restore command reports the destination:

```text
restored /data/errors.db
```

Continue only if the command succeeds. Drillip checks the backup and writes a
complete new database. It preserves existing destination files.

## 2. Start a separate server to check the data

Keep applications connected to their current server during a backup check.
Start the restored server with the image's defaults. Its port is available only
on the host's loopback address:

```bash
docker run --detach --name "$restored_container" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  --publish 127.0.0.1:18301:8300 \
  "$restore_image" serve
```

This example does not supply email or external telemetry settings to the test
server. Check health after the container starts:

```console
$ restored_url=http://127.0.0.1:18301
$ drillip --server "$restored_url" health
ok
```

If the server is still starting, repeat the health command. Use
`drillip --server "$restored_url" list` and `show` to check errors you expect in
the backup. See the [CLI reference](../reference/cli.md).

Stop the test server after you have checked the data:

```console
$ docker stop --timeout 30 "$restored_container"
drillip-restore-check
```

The restored volume remains available. If you only wanted to check the backup,
stop here.

## 3. Use the restored volume for recovery

Pause application and operator writes. The commands below replace the Docker
deployment from [Run Drillip](run-drillip.md#docker). Set `production_container`
to its container name or ID. For another deployment, use its saved startup
configuration with the restored volume and matching image.

If the old container is available, stop and remove it. These commands keep its
data volume. If the old container is gone, skip this block:

```bash
production_container=drillip
docker stop --timeout 30 "$production_container"
docker rm "$production_container"
```

Choose a name for the recovered container. The example below uses the settings
from the Docker guide. Add the environment settings and other options from your
saved configuration before you run it:

```bash
docker run --detach --name drillip \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  --publish 127.0.0.1:8300:8300 \
  "$restore_image" serve
```

Check health at the recovered server's address:

```console
$ drillip --server http://127.0.0.1:8300 health
ok
```

Check the expected data again before you resume writes. Keep the original volume
and backup until you have checked the recovered deployment.
