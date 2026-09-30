# Back up Drillip data

Use this guide to save a database backup from a running Drillip server.
To check an existing backup or recover data, use
[Restore Drillip data in Docker](restore-backup.md). That procedure does not
need access to the source server.

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

## Check or restore the backup

Use [Restore Drillip data in Docker](restore-backup.md) to check the backup in a
separate server or recover a deployment. The procedure keeps the backup and the
original volume.
