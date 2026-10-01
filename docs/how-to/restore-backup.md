# Restore Drillip data in Docker

Use a database backup to recover a Docker deployment. Restore the data, check it,
then start the recovered server. You do not need the source server.
The backup file and the original data volume stay unchanged.

## Before you start

Have these ready:

- A file created by `drillip backup`; see [Back up Drillip data](backup-restore.md).
- Docker and a Drillip image built from this checkout; see the
  [build instructions](upgrade-cli.md#1-build-and-replace-the-executable).
- Your deployment's Docker startup command, including environment variables and
  network settings.

The examples use the image tag `drillip:local`.
The data checks use the Drillip client inside the container.

Run the commands in the same Bash session. In terminal examples, `$` marks a
command. Lines without `$` show example output.

## 1. Restore the data

Put the backup file in your current directory as `drillip-backup.db`.
Set `restore_image` to your image tag. Choose an unused name for `restored_volume`:

```bash
restore_image=drillip:local
restored_volume=drillip-restored
```

Restore the file to the new volume. Docker creates the volume:

```console
$ docker run --rm --interactive \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  "$restore_image" restore --input - < drillip-backup.db
restored /data/errors.db
```

Continue only if restore succeeds. Drillip checks the backup before it creates
the database. It keeps the backup file and refuses an existing database.

## 2. Check the data

Choose an unused container name. Start a temporary server with the restored
volume:

```bash
restored_container=drillip-restore-check
docker run --detach --name "$restored_container" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  "$restore_image" serve
```

This command publishes no host port and supplies no email or external telemetry
settings.

Check the restore time and the backup's data time. The timestamps below are examples:

```console
$ docker exec "$restored_container" /drillip health --details
status: ok
last_backup_generated_at: unknown
last_restored_at: 2026-09-30T12:02:00Z
restored_snapshot_at: 2026-09-30T12:00:00Z
```

If the server is still starting, repeat the health command.
Check `restored_snapshot_at` against the time of the backup you selected.
`last_restored_at` is the time of this restore.

Older backups can show `unknown` for the data time.
`last_backup_generated_at` is `unknown` until this server creates a backup.

Check an error that you expect in the backup. Replace the example
[fingerprint](../reference/glossary.md#fingerprint) with your error's fingerprint.
The values below are examples; `...` marks omitted output:

```console
$ docker exec "$restored_container" /drillip show 63fe27befddcf06b
...
Fingerprint: 63fe27befddcf06b
Level:       error
Type:        message
Value:       Backup validation open
Count:       3
...
```

Check the fingerprint, message, and count against the history you expect.
For an empty backup, use `docker exec "$restored_container" /drillip stats` to
check its zero totals.
Health alone does not confirm that you selected the right backup.

Stop the test server after you have checked the data:

```console
$ docker stop --timeout 30 "$restored_container"
drillip-restore-check
```

The restored volume remains available for the recovered server.

## 3. Resume operation

Pause applications and scripts that send events or change Drillip data.
The commands below replace the container
from [Run Drillip](run-drillip.md#docker). Set `production_container` to your
container's name or ID.

If the old container is available, stop and remove it. These commands keep its
data volume. If the old container is gone, skip this block:

```bash
production_container=drillip
docker stop --timeout 30 "$production_container"
docker rm "$production_container"
```

Start the recovered server with the restored volume. Keep the environment
variables, network settings, and other options from your deployment's startup
command. The Docker guide's deployment needs this command:

```bash
docker run --detach --name drillip \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  --publish 127.0.0.1:8300:8300 \
  "$restore_image" serve
```

Check the recovered server. Its restore times must match those from step 2:

```console
$ docker exec drillip /drillip health --details
status: ok
last_backup_generated_at: unknown
last_restored_at: 2026-09-30T12:02:00Z
restored_snapshot_at: 2026-09-30T12:00:00Z
```

Repeat your error check in this container. Resume writes only after the time and
content checks pass. Keep the original volume and backup until recovery is confirmed.
