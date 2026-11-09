# Architecture

dmsx is one process. It backs up MySQL, MariaDB, PostgreSQL and MongoDB, archives old MySQL rows, answers cold queries, and runs online DDL. The browser talks to that same process.

The shape follows the decisions in Databasus that stay useful for a single-operator tool: one process owns the work, dump bytes never land in a local staging file, engine code does not know about HTTP, and secrets are encrypted at rest. dmsx implements those decisions in its own packages. It does not vendor the Databasus source tree.

## System

```
browser
   │
   ▼
dmsx process
   HTTP, scheduler, embedded pages
   SQLite catalog + master key
   │
   ├── database client tools (mysqldump, pg_dump, pg_basebackup, mongodump, gh-ost)
   └── storage (S3, Qiniu, local, SFTP, FTP, Azure, Drive, SMB, rclone)
```

- One process. There is no worker, no separate admin service, and no internal PostgreSQL.
- The catalog in `DMSX_DATA` remembers connections, plans and outcomes. Backup payloads are not rows in that database.
- Database client tools come from the machine, or from the container image. dmsx does not ship a copy of every server version. The package script embeds the official gh-ost release into the dmsx executable.
- Backups run from this process over the network, or through an SSH local port forward. Nothing of dmsx is installed on the database host.

## Layout

Dependencies point one way: `cmd/dmsx` → `internal/app` → engines, storage, backup, archive, cold query, online DDL → small libraries.

| Path | Owns |
| --- | --- |
| `cmd/dmsx` | process entry, listen address, data directory |
| `internal/app` | catalog records, scheduling, job lifecycle |
| `internal/server` | HTTP, session cookie, embedded pages |
| `internal/engine` | native dump and restore tools |
| `internal/backup` | the pipe from dump stdout to storage |
| `internal/storage` | destinations |
| `internal/schedule`, `internal/retention` | when a plan runs, which copies stay |
| `internal/streamcrypt`, `internal/secret` | backup encryption and field encryption |
| `internal/archive`, `internal/coldquery`, `internal/onlineddl` | the other two jobs |

`internal/engine` does not import `internal/app` or `internal/server`. A storage implementation does not know which database produced the bytes.

## Whole-database backup

A due plan starts one backup. The native tool writes to a pipe. Plain SQL is compressed with zstd in that pipe. An optional AES-GCM stream seals the bytes. Storage reads the pipe and uploads it. A slow upload stalls the dump. The disk does not hold a second full copy.

Engines:

- MySQL and MariaDB: `mysqldump` / `mariadb-dump`, SQL, pipeline zstd
- PostgreSQL logical: `pg_dump -Fc`
- PostgreSQL physical: `pg_basebackup` tar on stdout, including the WAL fetched for that base backup
- MongoDB: `mongodump --archive --gzip`

What the operator can do with a finished backup:

- download it
- delete the record and the stored object
- restore MySQL, MariaDB, PostgreSQL custom dumps and MongoDB archives into a target that is not saved
- leave a physical base backup as a tar download

A plan can be hourly, daily, weekly, monthly or cron. Retention is a count, a time window, or GFS. A failed plan retries until its limit. Deleting a database deletes its plans, its backup records and its backup objects.

The size of a running backup is written back about every five seconds.

Continuous WAL archiving and point-in-time recovery are not this pipeline. A physical backup here is one base-backup stream.

## Sign-in

The operator password is hashed in the catalog. The browser keeps an HttpOnly session cookie. The password is not an environment variable and not a line in a config file. `master.key` encrypts stored connection secrets and backup streams. It is not the sign-in password.

## What stays out

Workspaces, OAuth, notifiers, a verification agent, and OpenSpec review gates belong to Databasus. They are not part of this process.
