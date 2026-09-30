# Back up and restore a Docker database volume

Use this guide to back up a Drillip volume and check the restored data on the
same Docker host. The backup requires a short server outage.

## Before you start

You need:

- Docker, Bash, `curl`, and `jq`.
- A Drillip container with a named volume mounted at `/data` and its database at
  `/data/errors.db`. These steps were checked with v0.3.18 and the image's default user.
- A container that remains available after it stops. Do not use `--rm` for the source container.
- Enough disk space for the backup and a restored volume.
- A free host port, `18301`, for the restore check.

The source container must be the only process that uses the volume. Stop any
database maintenance commands. Suspend any job that could restart the container.

Save your deployment configuration separately. The volume does not contain
container settings or credentials supplied through the environment.
Keep the backup under the same access controls as the database.

Run the commands in the same Bash session. Continue only if each command succeeds.
In `console` blocks, `$` marks a command. Lines without `$` show example output.

## 1. Select the source

Use `docker ps` to find the source container. Replace the container name, volume
name, and server URL below with your values:

```bash
set -euo pipefail
source_container=drillip
source_volume=drillip-data
source_url=http://127.0.0.1:8300
umask 077
backup_dir=$(mktemp -d "$PWD/drillip-backup.XXXXXX")
image=$(docker inspect --format '{{.Image}}' "$source_container")
printf '%s\n' "$image" > "$backup_dir/image-id.txt"
docker volume inspect "$source_volume" >/dev/null
docker inspect --format '{{range .Mounts}}{{println .Name .Destination}}{{end}}' \
  "$source_container"
docker pull alpine:3.23
```

Check that the mount list shows your volume at `/data`. For the example values,
the line is:

```text
drillip-data /data
```

The Alpine image supplies `tar`. Download it before you stop the server.
The `image-id.txt` file records the exact local Drillip image for the restore check.

## 2. Record the current data

Pause error reports from your applications. Pause operator commands that
change stored data. Keep these writes paused until step 4.

Define a function to record group fingerprints, counts, resolution states,
and the total number of stored occurrences. Then record the source data:

```bash
snapshot() {
  local url=$1 prefix=$2 limit
  curl --fail --silent --show-error "$url/api/0/stats/" |
    jq '{unique_errors, total_occurrences}' > "$prefix.stats.json"
  limit=$(jq '[.unique_errors, 1] | max' "$prefix.stats.json")
  curl --fail --silent --show-error "$url/api/0/top/?limit=$limit" |
    jq 'map({fingerprint, count, resolved: (.state == "resolved")}) |
        sort_by(.fingerprint)' > "$prefix.groups.json"
}
snapshot "$source_url" "$backup_dir/before"
```

The function saves two comparison files in the backup directory.
It records whether a group is resolved. An unresolved group can change from
`new` to `ongoing` during the outage; see the
[lifecycle explanation](../explanation/error-lifecycle.md).

## 3. Stop the server and create the backup

Stop the source container. Check its exit status and check for other containers
that use the volume. Then copy the complete volume:

```bash
docker stop --timeout 30 "$source_container"
test "$(docker inspect --format '{{.State.Status}}:{{.State.ExitCode}}' \
  "$source_container")" = exited:0
test -z "$(docker ps --quiet --filter volume="$source_volume")"
docker run --rm \
  --mount "type=volume,src=$source_volume,dst=/data,readonly" \
  alpine:3.23 tar -C /data -czf - . > "$backup_dir/data.tar.gz"
docker run --rm -i alpine:3.23 tar -tzf - \
  < "$backup_dir/data.tar.gz" > "$backup_dir/files.txt"
printf 'Backup directory: %s\n' "$backup_dir"
```

The final command prints the backup directory. Your directory name will differ:

```text
Backup directory: /home/operator/drillip-backup.A1b2C3
```

If a command fails, do not use the backup. Restart the source container with
step 4 before you inspect the failure. If Bash exited, open a new session and
set `source_container` and `source_url` again.

Keep all files in the backup. Do not copy only `errors.db` or delete its SQLite
write-ahead log files. See [SQLite's database backup requirements](https://sqlite.org/wal.html#the_wal_file).

## 4. Restart the source server

Start the source container and check its health:

```console
$ docker start "$source_container"
drillip
$ curl --fail --silent --show-error --write-out '\n' \
    --retry 10 --retry-connrefused --retry-delay 1 "$source_url/-/healthy"
ok
```

The first output line is your container's name. Resume application and operator
writes after the health check succeeds.

Keep the complete backup directory, including its comparison files and image ID.
Copy it to your backup storage. Keep the recorded Drillip image available for the restore check.

## 5. Restore the backup to a new volume

Create a new volume and extract the backup. Start a test container with the
recorded Drillip image:

```bash
restore_id="$(date -u +%Y%m%dT%H%M%S)-$$"
restored_volume="drillip-restored-$restore_id"
restored_container="drillip-restore-check-$restore_id"
if docker volume inspect "$restored_volume" >/dev/null 2>&1; then
  printf 'Choose a new destination volume name.\n' >&2
  exit 1
fi
docker volume create "$restored_volume"
docker run --rm -i \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  alpine:3.23 tar -C /data -xzf - < "$backup_dir/data.tar.gz"
docker run --detach --name "$restored_container" \
  --mount "type=volume,src=$restored_volume,dst=/data" \
  --publish 127.0.0.1:18301:8300 \
  "$image" serve --listen 0.0.0.0:8300 --db /data/errors.db
```

The restored files keep their ownership and permissions. The test container uses
the image's default user. It has no email or external telemetry configuration.
Keep applications connected to the source server during this check.

## 6. Check the restored data

Set the test server URL and check its health:

```console
$ restored_url=http://127.0.0.1:18301
$ curl --fail --silent --show-error --write-out '\n' \
    --retry 10 --retry-connrefused --retry-delay 1 "$restored_url/-/healthy"
ok
```

Record the restored data. Compare it with the files from step 2:

```console
$ snapshot "$restored_url" "$backup_dir/after"
$ diff -u "$backup_dir/before.stats.json" "$backup_dir/after.stats.json"
$ diff -u "$backup_dir/before.groups.json" "$backup_dir/after.groups.json"
```

All three commands must succeed without output. This confirms matching group
fingerprints, counts, resolution states, and the total number of stored occurrences.

If `diff` shows a difference, investigate it before you use the restored data.
Writes or scheduled server tasks between step 2 and shutdown can change the data.
A successful health check alone does not confirm that the data matches.

## 7. Stop the test container

```console
$ docker stop --timeout 30 "$restored_container"
drillip-restore-check-20260930T120000-12345
```

The output shows your test container's name. The restored volume remains available.

To use the restored volume for recovery:

1. Pause application and operator writes.
2. Stop the current Drillip server.
3. Start your normal deployment with the restored volume, recorded image, and saved configuration.
4. Check server health before you resume writes.

Keep the original volume and backup until you have checked the recovered deployment.
