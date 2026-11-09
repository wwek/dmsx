---
name: whole-backup
description: Whole-database backup behavior for dmsx. Use when changing dump tools, the backup pipe, storage upload, schedules, retention, restore, or backup deletion.
---

# Whole-database backup

dmsx reimplements the backup path. Do not paste Databasus packages into this module.

## Path

Native tool stdout goes into one pipe:

`mysqldump` / `mariadb-dump` / `pg_dump` / `pg_basebackup` / `mongodump` → zstd only for plain SQL → optional AES-GCM → storage.

The storage reads the pipe. A slow upload must stall the dump. Do not add a local staging file for the backup body.

Encrypted streams use the header `DMSXBK01`. They are not Databasus files.

## Engines

- MySQL and MariaDB write SQL. Passwords go in a mode `0600` client file, not process arguments.
- PostgreSQL logical backup is `pg_dump -Fc`. Schema include and table exclude flags stay on that command.
- PostgreSQL physical backup is `pg_basebackup` with tar on stdout. It is not WAL streaming and not point-in-time recovery.
- MongoDB is `mongodump --archive --gzip`.
- Online DDL prefers the gh-ost binary embedded by `scripts/package.sh`. That file is the official GitHub release, checked against `SHA256SUMS`. `DMSX_GH_OST`, a `gh-ost` beside the executable, then `PATH` are the other lookups. `pt-online-schema-change` is not bundled.

## After the backup

- A running backup records its byte count about every five seconds.
- Retention runs after a successful backup: count, time window, or GFS.
- A failed enabled plan retries until `maxFailedTries`, with a short gap.
- Delete removes the catalog row and the stored object.
- Delete of a database removes its plans, backup rows and backup objects.
- Restore is allowed for `mysql-sql`, `mariadb-sql`, `pg-custom` and `mongo-archive`. The target password is not stored.
- A `pg-base*` object is downloaded as tar. Do not pipe it into a live server.

## Schedule

Hourly, daily, weekly, monthly and five-field cron. A cron expression that can never fire is rejected. A plan that has never run is due immediately, except that impossible cron.
