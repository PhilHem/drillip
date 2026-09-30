# Back up and restore a Docker database volume

Use this procedure to archive an existing Drillip named volume and verify its
restoration into a new volume. It requires a short tracker outage while copying
the stopped volume. Events sent during that outage might not be delivered;
pause senders or arrange application-side buffering first.

These commands cover the standard Docker deployment with `/data/errors.db`,
Drillip v0.3.15, and the image's default user. You need Docker, Bash, `curl`,
`jq`, and enough space for the archive and restored volume. Use the same Bash
session throughout. The source container must remain present after stopping
(no `--rm`), and must be the only process using the volume. Suspend any job or
orchestrator that could restart it or run local database maintenance.

The archive contains database data, including stored event context. Keep its
access as restricted as the tracker. Save your deployment configuration separately;
the volume does not contain container options or externally supplied credentials.

## 1. Select the source and prepare a backup directory

Find the source container with `docker ps`, then set its actual name or ID,
volume name, and reachable URL:

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

Confirm that the selected volume is mounted at `/data`. The helper image supplies
`tar`; pulling it now avoids adding download time to the outage. The image ID
records the exact locally available Drillip build to use for this restore check.

## 2. Record the data to check after restoration

Pause senders and other operator writes. Record all group fingerprints, counts,
resolution status, and the number of retained occurrences:

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

The comparison records whether each group is resolved. An unresolved group's
displayed state can age from `new` to `ongoing` while the tracker is stopped;
that is not lost state. See [the lifecycle explanation](../explanation/error-lifecycle.md).

## 3. Stop the owner and archive the whole volume

```bash
docker stop --time 30 "$source_container"
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

Continue only if every command succeeds. If the exit check fails, inspect the
container logs and establish why shutdown failed before accepting a backup.
The container check does not detect host processes: the sole-owner prerequisite
also excludes host-side SQLite or maintenance commands.

Drillip uses SQLite WAL mode. Copying only a live `errors.db` can omit committed
transactions. The archive above copies the entire stopped volume, including any
remaining `errors.db-wal` and `errors.db-shm` files; do not remove those files by
hand. SQLite documents why the [WAL must remain with its database](https://sqlite.org/wal.html#the_wal_file).
For a backup without an outage, use a tool implementing
[SQLite's Online Backup API](https://sqlite.org/backup.html); plain live-file
copying is not an equivalent procedure.

The copy is complete. Restart the original tracker and resume senders after its
health check succeeds:

```bash
docker start "$source_container"
curl --fail --silent --show-error \
  --retry 10 --retry-connrefused --retry-delay 1 "$source_url/-/healthy"
```

Expect `ok`. Keep the backup directory, including its comparison files, outside
the database volume. Copy it to your backup storage under the same access controls.

## 4. Restore into a fresh volume

Use unused names and a free host port `18301`. The original volume and archive
remain intact. For a later restore, set `backup_dir` to the saved backup directory
and load `image` from its `image-id.txt`; that exact image must be available on
the Docker host.

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

Extraction preserves the volume files' numeric ownership and permissions. The
standard image can use these unchanged. If your deployment uses a custom user,
restore with that same UID/GID and ensure it can write both the database and its
directory; do not solve a permission failure with world-writable permissions.

This verification container uses the original image and restored data, without
SMTP or remote telemetry configuration. Keep applications pointed at the original
tracker during the check. Recreating the production deployment later requires
its separately saved configuration.

## 5. Verify the restored data

```bash
restored_url=http://127.0.0.1:18301
curl --fail --silent --show-error \
  --retry 10 --retry-connrefused --retry-delay 1 "$restored_url/-/healthy"
snapshot "$restored_url" "$backup_dir/after"
diff -u "$backup_dir/before.stats.json" "$backup_dir/after.stats.json"
diff -u "$backup_dir/before.groups.json" "$backup_dir/after.groups.json"
```

Expect `ok` and no differences: the same fingerprints, occurrence counts,
resolution status, and retained occurrence total. Investigate any difference
before using the restored copy. Writes between the baseline and shutdown, or
scheduled lifecycle maintenance, can change these values; health alone does not
verify a restore. A group count can exceed retained occurrences because of
[retention](../explanation/error-lifecycle.md#retention-removes-occurrence-history).

Stop the verification container when finished:

```bash
docker stop --time 30 "$restored_container"
```

The restored volume remains available. For recovery, stop the current tracker,
point your normal deployment at this volume, and use the saved configuration and
matching image. Check health before resuming senders. Keep the original volume
and backup until the restored deployment is verified; none of the commands above
delete them.
