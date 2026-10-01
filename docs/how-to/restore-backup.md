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
matching image. Choose an unused name for `restored_volume`:

```bash
backup_file="$PWD/drillip-backup.db"
restore_image=drillip:local
restored_volume=drillip-restored
```

Run restore with the Drillip image. Docker creates the volume. The backup file
is mounted read-only:

```console
$ docker run --rm \
  --mount "type=bind,src=$backup_file,dst=/backup.db,readonly" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  "$restore_image" restore --input /backup.db
restored /data/errors.db
```

Continue only if restore succeeds. Drillip checks the backup and creates the
new database. It refuses to overwrite an existing database.

## 2. Start a separate server to check the data

Keep applications connected to their current server during a backup check.
Choose an unused container name and start the restored server with the image's
defaults. Its port is available only on the host's loopback address:

```bash
restored_container=drillip-restore-check
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

If the server is still starting, repeat the health command. Health confirms that
the database is reachable. It does not confirm that you selected the right backup.

Check the restore time and the backup's data time. These timestamps are examples:

```console
$ drillip --server "$restored_url" health --details
status: ok
last_backup_generated_at: unknown
last_restored_at: 2026-09-30T12:02:00Z
restored_snapshot_at: 2026-09-30T12:00:00Z
```

`last_restored_at` records this restore. `restored_snapshot_at` records the data
time, so it can be earlier. Older backups show `unknown` for the data time.
The new database has not generated a backup yet.

Check an error that you expect at that data time. Replace the example
[fingerprint](../reference/glossary.md#fingerprint) with a known fingerprint from
your backup. This excerpt uses example data and omits other output fields:

```console
$ fingerprint=63fe27befddcf06b
$ drillip --server "$restored_url" show "$fingerprint"
...
Fingerprint: 63fe27befddcf06b
Level:       error
Type:        message
Value:       Backup validation open
Count:       3
...
```

Check the fingerprint, message, and count against the history you expect. Use
`list` to find more errors and `silences` to check notification silences. If the
backup should be empty, use `stats` to check its zero totals. See the
[CLI reference](../reference/cli.md) for those commands. A successful integrity
check and health response do not replace these content checks.

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

Repeat the `health --details` and content checks at the recovered server's address
before you resume writes. Keep the original volume
and backup until you have checked the recovered deployment.
