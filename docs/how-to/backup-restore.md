# Back up and restore Drillip data

Use this guide to save a database backup from a running Drillip server.
You can restore the backup into a new Docker volume to check it or recover data.

## Before you start

Use a client and server built from this checkout. For an older installation,
[update the server and client](upgrade-cli.md). The client checks whether the
server supports backups before it downloads data.

Save your deployment configuration separately. The database backup does not
contain container settings or credentials supplied through the environment.
Keep the backup under the same access controls as the database.

Run the commands in the same Bash session. In terminal examples, `$` marks a
command. Lines without `$` show example output.

## 1. Select the server

Replace the example URL with your server's URL:

```console
$ export DRILLIP_SERVER=http://127.0.0.1:8300
$ drillip health
ok
```

For a remote server, you can use an
[SSH tunnel](upgrade-cli.md#2-select-and-check-the-server).

## 2. Save the backup

Choose a new filename. The command does not replace existing files:

```console
$ drillip backup --output drillip-backup.db
saved drillip-backup.db
```

The server creates and checks a consistent database snapshot while it runs.
The backup includes error groups, stored occurrences, context, and silences.
The client saves the file only after the complete download succeeds.
It gives the file read and write permissions for its owner.

Copy the file to your backup storage. Record the server version and keep its
matching image available for restoration. To schedule backups, run the same
command from your existing scheduler with a different filename for each backup.

## 3. Restore into a new Docker volume

You need Docker, the Alpine helper image, and a Drillip image from the same build
as the source server. This example uses `drillip:local` from the
[checkout build instructions](upgrade-cli.md#1-build-and-replace-the-executable)
and the image's default user. Host port `18301` must be free.

Keep applications connected to the current server during the restore check.
Replace `restore_image` with your matching image. Run this script to create an
unused volume and copy the backup into it:

```bash
set -euo pipefail
backup_file="$PWD/drillip-backup.db"
restore_image=drillip:local
restore_id="$(date -u +%Y%m%dT%H%M%S)-$$"
restored_volume="drillip-restored-$restore_id"
restored_container="drillip-restore-check-$restore_id"
if docker volume inspect "$restored_volume" >/dev/null 2>&1; then
  printf 'Choose a new destination volume name.\n' >&2
  exit 1
fi
docker volume create "$restored_volume"
docker run --rm \
  --mount "type=bind,src=$backup_file,dst=/backup.db,readonly" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  alpine:3.23 cp /backup.db /data/errors.db
docker run --detach --name "$restored_container" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  --publish 127.0.0.1:18301:8300 \
  "$restore_image" serve --listen 0.0.0.0:8300 --db /data/errors.db
```

Continue only if each command succeeds. The test container uses the restored
volume. It has no email or external telemetry configuration.

## 4. Check the restored server

Check health after the test container starts:

```console
$ restored_url=http://127.0.0.1:18301
$ drillip --server "$restored_url" health
ok
```

If the server is still starting, repeat the health command.
Use `drillip --server "$restored_url" list` and `show` to check errors you
expect in the backup. See the [CLI reference](../reference/cli.md).

Stop the test container after the check:

```console
$ docker stop --timeout 30 "$restored_container"
drillip-restore-check-20260930T120000-12345
```

The output shows your test container's name. The restored volume remains available.

## 5. Use the restored volume for recovery

Pause application and operator writes. Stop the current Drillip server.
Start your normal deployment with the restored volume, matching image, and saved
configuration. Check server health before you resume writes.

Keep the original volume and backup until you have checked the recovered deployment.
