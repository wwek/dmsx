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
- PostgreSQL physical backup is `pg_basebackup` with tar on stdout. While the tar streams to storage, dmsx rebuilds a `backup_manifest` sidecar (hashed members + LSN range) so a later run can pass `--incremental`.
- Optional WAL archiving uses `pg_receivewal` into a short local queue under the data directory, then uploads completed segments. That queue is the only intentional staging exception.
- Logical `pg_restore` stages the custom dump for that restore call only. It passes `--no-owner` and `--no-acl`, and a filtered TOC list drops extension and user-mapping entries. `-j` is used only when parallel is above 1.
- MongoDB is `mongodump --archive --gzip`.
- Online DDL prefers the gh-ost binary embedded by `scripts/package.sh`. That file is the official GitHub release, checked against `SHA256SUMS`. `DMSX_GH_OST`, a `gh-ost` beside the executable, then `PATH` are the other lookups. `pt-online-schema-change` is not bundled.

## After the backup

- A running backup records its byte count about every five seconds.
- Retention runs after a successful backup: count, time window, or GFS.
- A failed enabled plan retries until `maxFailedTries`, with a short gap.
- Delete removes the metadata repo row and the stored object.
- Delete of a database removes its plans, backup rows and backup objects.
- Restore is allowed for `mysql-sql`, `mariadb-sql`, `pg-custom` and `mongo-archive`. The target password is not stored.
- A restore can request `parallel` (`pg_restore -j`) and `verify` (post-restore table count).
- A `pg-base*` object is downloaded as tar. Do not pipe it into a live server; use the PITR guide beside WAL segments.

## Schedule

Hourly, daily, weekly, monthly and five-field cron. A cron expression that can never fire is rejected. A plan that has never run is due immediately, except that impossible cron.
