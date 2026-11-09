# dmsx

[中文](README.zh-CN.md)

Back up a whole database, archive old rows, and change the shape of a large table. All of that happens in one page.

**Status: self-evaluation**

The project is in the author's self-evaluation. Features have been checked by the author. There has been no independent audit, and this is not an accepted release.

Source: https://github.com/wwek/dmsx

## Disclaimer

dmsx exports, deletes, and replaces data in the databases you point it at. After an archive is confirmed, it deletes those rows from the primary database. An online schema change swaps out the live table. A wrong storage setting, a backup that stops halfway, or a restore you never rehearsed can leave the data unrecoverable.

By using it, you accept the full consequence of loss, corruption, and failed recovery. The author and contributors give no warranty, and they do not accept claims of the form “I used dmsx and the data is gone.” Rehearse backup and restore on a copy before you delete rows or cut over a production table.

## Features

### Whole-database backup

Send a full database to storage you choose. Run it on a schedule, drop older copies by a retention rule, and restore when you need the data back.

The machine running dmsx does not keep a second full copy of the backup on its own disk.

### Archive old rows

Move old rows of one table to archive storage, using a condition such as time. After the archive file is confirmed complete, those rows are removed from the primary database so the live disk can be reused.

### Look up history

After a table is archived, you can still look up those rows in the page, with your own filter. The query reads the part you asked for. You do not download the whole archive first.

### Change a large table

Altering a table with a huge number of rows can stall the application. Submit the change in the page and it finishes in the background, with progress and an estimated time remaining. The live table stays in place until you confirm the cut-over.

### One page

Manage databases, storage, backups, archives, history lookup, and schema changes in the browser. The first visit sets a password. Later visits sign in with it. Keep the page on the local machine.

## Start

Start it one of the ways below, then open http://127.0.0.1:8080 . Set a password on the first visit, and sign in with it after that. Add a database and a storage destination, then create a backup, an archive, or a schema change.

### Download a binary

Download the archive for your system from [Releases](https://github.com/wwek/dmsx/releases), unpack it, and run:

```bash
./dmsx
```

This install uses the database client tools already on the machine. Backups and schema changes run only when those tools are present.

### Run a container

```bash
docker run -d --name dmsx \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e DMSX_ADDR=0.0.0.0:8080 \
  -v dmsx-data:/var/lib/dmsx \
  ghcr.io/wwek/dmsx:latest
```

The image includes the usual database client tools. The page is published only on localhost port 8080. Settings and job records stay in the `dmsx-data` volume.

## Technical inventory

| Item | Scope |
| --- | --- |
| Databases | MySQL, MariaDB, PostgreSQL (logical and physical), MongoDB |
| Storage | S3, Qiniu, a local directory, SFTP, FTP, Azure Blob, Google Drive, SMB, rclone |
| Jobs | Scheduled backup, retention, restore, single-table archive, history lookup, online schema change |
| Page | `127.0.0.1:8080` by default, override with `DMSX_ADDR` |
| Data directory | `DMSX_DATA`. The binary defaults to `./data`. The container uses `/var/lib/dmsx` |
| Sign-in | One operator password. The hash lives in the data-directory database, and the session is a browser cookie. The password is not stored in a config file or an environment variable |

A binary install needs `mysqldump`, `mariadb-dump`, `pg_dump`, `pg_basebackup` and `mongodump` on the machine. `scripts/package.sh` downloads the official [gh-ost](https://github.com/github/gh-ost) release, checks its checksum, embeds it in `dist/dmsx`, and also writes `dist/gh-ost`. `pt-online-schema-change` is still installed separately. Point at a specific binary with variables such as `DMSX_MYSQLDUMP` and `DMSX_GH_OST`.

## Core technical notes

### Backup

Dump-tool stdout follows one pipe: `mysqldump` / `mariadb-dump` / `pg_dump` / `pg_basebackup` / `mongodump` → zstd for plain SQL → optional AES-GCM → storage. Encrypted files start with the header `DMSXBK01`.

S3 and Qiniu keep a single 16 MiB part in flight, roll to the next object at 1000 parts, and write the manifest last. PostgreSQL physical backup is `pg_basebackup --pgdata=- --format=tar`. Passwords go into a mode `0600` client file, not onto the process arguments. SSH is a local port forward. A plan that has never run is due immediately.

This path follows the design published by [Databasus](https://github.com/databasus/databasus). dmsx reimplements it and does not copy that source tree. The files are not interchangeable with Databasus backups. See [NOTICE.md](NOTICE.md). Process boundaries are in [docs/architecture.md](docs/architecture.md).

### Archive and history lookup

A MySQL cursor streams the `WHERE` rows into an in-memory Parquet file and uploads one object:

`dms_archives/<database>/<table>/<year>_archive.parquet`

dmsx re-reads the object and checks the MD5 before deleting rows, 5000 at a time. A later archive for the same year is stored beside the earlier file.

History lookup uses in-process DuckDB. S3 and Qiniu are read through httpfs. A local destination is read from the directory. A filter that contains `;`, a comment, or `read_` is rejected. Results use `LIMIT 100`.

### Online schema change

dmsx runs `gh-ost` or `pt-online-schema-change`. The tool owns the ghost table, the chunk copy, binlog catch-up, and the table rename. dmsx reads progress and the estimated time remaining from stdout and pushes them to the page over WebSocket. `--execute` stays off unless the console checkbox is set.

## Development

Running from source and running tests belong here. Day-to-day use is the binary or the container above.

```bash
go run ./cmd/dmsx
go test ./...
go test -tags e2e ./e2e
```

GitHub Actions runs both on push and pull request.

Go 1.26 is required. The e2e test starts MySQL 8.4 with `e2e/docker-compose.yml`, then runs a logical backup, an archive that deletes rows, and a history lookup.

## Thanks

- https://github.com/databasus/databasus
- https://github.com/wal-g/wal-g
- https://github.com/gobackup/gobackup
