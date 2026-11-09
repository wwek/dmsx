# dmsx agent notes

How the process is built is in [docs/architecture.md](docs/architecture.md). Product behavior for people using dmsx is in [README.md](README.md) and [README.zh-CN.md](README.zh-CN.md).

Reply to the person in the language they write in. Keep identifiers, code comments, log messages and this file in English. The embedded console and the Chinese README stay Chinese.

## Backup work

Before changing whole-database backup, restore, scheduling, retention, dump tools or storage streaming, read [.agents/skills/whole-backup/SKILL.md](.agents/skills/whole-backup/SKILL.md).

Implement behavior in this repository. Do not copy Databasus source into these packages. Databasus under `.local/databasus` is a reference for behavior, not a tree to vendor.

## Code

- A backup byte does not get written to a staging file on the way to storage.
- `internal/engine` does not import `internal/app` or `internal/server`.
- Connection passwords and storage secrets stay encrypted in the catalog. API responses do not return them.
- A restore target's password is used for that restore and is not stored.
- Names say what the code does. A comment is for a constraint the code cannot show.

## Checks

For a Go change, run `go test ./...`. The `e2e` package is behind the `e2e` build tag and needs Docker. Do not start it unless the change is about a live MySQL backup, archive or cold query.
