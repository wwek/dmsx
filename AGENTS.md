# dmsx agent notes

How the process is built is in [docs/architecture.md](docs/architecture.md). Product behavior for people using dmsx is in [README.md](README.md) and [README.zh-CN.md](README.zh-CN.md).

Reply to the person in the language they write in. Keep identifiers, code comments, log messages and this file in English. The embedded console is bilingual (zh/en); the Chinese README stays Chinese.

## Backup work

Before changing whole-database backup, restore, scheduling, retention, dump tools or storage streaming, read [.agents/skills/whole-backup/SKILL.md](.agents/skills/whole-backup/SKILL.md).

Implement behavior in this repository. You may read Databasus under `.local/databasus` as a behavior reference, then write the dmsx code yourself. Do not copy Databasus source into these packages.

The metadata repo is SQLite under `DMSX_DATA` (`internal/repo`). That is intentional; Databasus embeds PostgreSQL for its own metadata and dmsx does not follow that choice.

Table archive and cold query stay on MySQL and MariaDB. S3 and Qiniu archives use the storage prefix for upload, MD5 reread, and cold query. Google Drive is refused for archive. Online DDL is `gh-ost` or `pt-online-schema-change`, and it cuts over only when the operator asks. Do not add new product behavior on those paths unless asked.

## Code

- A backup byte does not get written to a staging file on the way to storage.
- `internal/engine` does not import `internal/app` or `internal/server`.
- Connection passwords and storage secrets stay encrypted in the metadata repo. API responses do not return them.
- A restore target's password is used for that restore and is not stored.
- Names say what the code does. A comment is for a constraint the code cannot show.
- The console source is `web/`. `internal/server/ui/` is its build output, not hand-written. After changing `web/src`, rebuild it with `npm run build` in `web/` and commit the result, so the embedded bytes never drift from the source.

## Checks

For a Go change, run `go test ./...`. The `e2e` package is behind the `e2e` build tag and needs Docker. Do not start it unless the change is about a live MySQL backup, archive or cold query.

Local runs keep the metadata repo and the local storage under `e2e/.local/`, which `.gitignore` already excludes. Do not put dmsx data under `/tmp`.

Every file a run creates stays inside this repository. Use `.local/` at the repository root, or `e2e/.local/` for e2e scratch space; both are already excluded by `.gitignore`. The metadata repo, local storage, scratch data, build output and logs all belong there. Do not write anything under `/tmp` or outside the repository.
