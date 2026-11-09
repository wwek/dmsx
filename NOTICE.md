Source repository: https://github.com/wwek/dmsx

Status: self-evaluation. The author has checked this project. It has no
independent audit and is not an accepted release.

Disclaimer. dmsx exports, deletes, and replaces database data. Archive jobs
delete source rows after upload, and online DDL renames the live table. A bad
storage setting, a failed backup, or an unrehearsed restore can make the data
unrecoverable. The operator accepts that loss. The author and contributors
give no warranty and do not accept claims that data disappeared because
someone used this tool. Rehearse on a copy before deleting or cutting over
production data.

dmsx is an independent program. It is not Databasus, and it is not affiliated
with, endorsed by, or sponsored by the Databasus project.

The backup data path follows the design published by Databasus under the
Apache License, Version 2.0: a native dump tool writes to a pipe, plain SQL is
compressed with zstd, an optional AES-GCM stream seals the result, and S3-compatible
storage uploads one 16 MiB part at a time, rolling into another object and a
manifest past the provider part cap. dmsx reimplements that path in its own
packages. It does not copy the Databasus source tree, and the on-disk encryption
magic is DMSXBK01 rather than the Databasus header.

Databasus is a trademark of Rostislav Dugin. The name is used here only to
identify the project whose published design this program follows:

https://github.com/databasus/databasus

Two other projects shaped the same idea of pushing a database to object storage
without a local backup repository. They are recorded here as thanks, not as
vendored code:

https://github.com/wal-g/wal-g
https://github.com/gobackup/gobackup

Online DDL is delegated to gh-ost (https://github.com/github/gh-ost) or
Percona Toolkit's pt-online-schema-change. Cold queries use DuckDB. Archived
tables are Apache Parquet, written with parquet-go.
