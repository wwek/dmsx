# Architecture

dmsx is one process. It backs up MySQL, MariaDB, PostgreSQL and MongoDB, archives old MySQL rows, answers cold queries, and runs online DDL. The browser talks to that same process over a Gin HTTP API. The console is a Vite + React build embedded from `internal/server/ui`.

The shape follows the decisions in Databasus that stay useful for a single-operator tool: one process owns the work, dump bytes never land in a local staging file, engine code does not know about HTTP, and secrets are encrypted at rest. dmsx reimplements those behaviors in its own packages. Databasus under `.local/databasus` is a reference for behavior, not a tree to copy.

## System

```
browser
   │
   ▼
dmsx process
   Gin HTTP API, scheduler, embedded pages
   SQLite metadata repo + master key
   │
   ├── database client tools (mysqldump, pg_dump, pg_basebackup, mongodump, gh-ost)
   └── storage (filesystem, S3, GCS, Azure, OSS, Swift, SSH, Qiniu)
```

- One process. There is no worker, no separate admin service, and no internal PostgreSQL.
- The metadata repo in `DMSX_DATA` remembers connections, plans and outcomes. Backup payloads are not rows in that database.
- Database client tools come from the machine, or from the container image. dmsx does not ship a copy of every server version. The package script embeds the official gh-ost release into the dmsx executable.
- Backups run from this process over the network, or through an SSH local port forward. Nothing of dmsx is installed on the database host.

## Layout

Dependencies point one way: `cmd/dmsx` → `internal/app` → engines, storage, backup, archive, cold query, online DDL → small libraries.

| Path | Owns |
| --- | --- |
| `cmd/dmsx` | process entry, listen address, data directory |
| `internal/app` | metadata repo records, scheduling, job lifecycle |
| `internal/repo` | SQLite metadata store (connections, plans, runs) |
| `internal/server` | Gin HTTP API, session cookie, embedded pages |
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
- PostgreSQL physical: `pg_basebackup` tar on stdout, including the WAL fetched for that base backup. A reconstructed `backup_manifest` sidecar enables PG 17+ incremental chains (`full_incr` plans).
- MongoDB: `mongodump --archive --gzip`

What the operator can do with a finished backup:

- download it
- delete the record and the stored object
- restore MySQL, MariaDB, PostgreSQL custom dumps and MongoDB archives into a target that is not saved. A PostgreSQL custom restore leaves ownership and grants to the restoring role, and skips extension and user-mapping entries.
- leave a physical base backup as a tar download

A plan can be hourly, daily, weekly, monthly or cron. Retention is a count, a time window, or GFS. A failed plan retries until its limit. Deleting a database deletes its plans, its backup records and its backup objects.

The size of a running backup is written back about every five seconds.

A plan, database or storage target can be edited in place. Empty secret fields on update keep the previous sealed values.

Physical plans can enable WAL archiving with `pg_receivewal`. Completed segments upload under `wal/{databaseId}/`. PITR is operator-driven from the base backup tar, incremental chain, and those segments.

## Sign-in

The operator password is hashed in the metadata repo. The browser keeps an HttpOnly session cookie. The password is not an environment variable and not a line in a config file. `master.key` encrypts stored connection secrets and backup streams. It is not the sign-in password.

## Notifications and health

The operator can attach Email, Telegram, Slack, Discord, Teams, Mattermost or webhook channels to backup, restore and health events. Each database can be probed on an interval; consecutive failures mark it unavailable and can notify.

An audit log keeps recent operator actions. Secrets are not written into it.

## What stays out for now

Workspaces, multi-user roles, OAuth, email 2FA, fully automated PITR cut-over, container-based restore verification agents, and OpenSpec review gates are not part of this process yet.

Table archive and cold query stay on MySQL and MariaDB. An archive uploads Parquet, reads it back for an MD5 check, and only then deletes source rows. S3 and Qiniu use the storage prefix on that upload, the reread, and the cold-query glob. `skipTLS` is passed through to the upload client and to DuckDB. Google Drive is refused because its reads are not implemented. Cold query is separate from the backup destination list. It reads a filesystem directory or an S3 API archive: S3, Qiniu, Alibaba Cloud OSS, or GCS. Azure, Swift and SSH store backups but are not opened by cold query.

Online DDL runs `gh-ost` or `pt-online-schema-change` against MySQL or MariaDB. The history shows the tool, whether the run was a dry run or a cut-over, the chunk size, and the saved log. A run does not cut over unless the operator asks it to.
