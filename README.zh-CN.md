# dmsx

[English](README.md)

给数据库做整库备份、把历史数据归档、以及给大表改结构。这些都在同一个页面里完成。

**状态：自评**

当前处于作者自评。功能由作者自行核对，尚未经过独立审计，也尚未作为已验收版本对外发布。

代码仓库：https://github.com/wwek/dmsx

## 免责声明

dmsx 会导出、删除和替换你指定数据库里的数据。归档确认完成后，会从主库删掉已经归档的行。在线改表会把正在使用的表换掉。存储配错、备份中断，或恢复之前没有自己验证过，数据都可能找不回来。

使用即表示数据丢失、损坏和无法恢复的后果由操作者自行承担。作者和贡献者不作任何保证，也不接受「用了 dmsx，数据没了」这类追责。先在副本上验证备份和恢复，再对生产库做删除或切表。

## 功能特性

### 整库备份

把整个库备份到你自己的存储，可以定时执行，也可以按保留规则清理更早的备份。需要时再恢复回去。

备份过程中，运行 dmsx 的机器上不会再存一份完整文件。

### 历史数据归档

按时间等条件，把一张表里的旧数据搬到归档存储。确认归档文件完整之后，再从主库删掉这些行，把空间还给线上库。

### 回看历史数据

归档之后，仍可以在页面里按条件查看这些数据。查询只取你要的那一部分，不用先把整份归档下载到本地。

### 大表改结构

在行数很多的表上直接改结构，容易把业务卡住。在页面提交变更后，后台平滑完成切换，页面上能看到进度和预计剩余时间。你确认执行之前，不会切到新表。

### 一个页面

在浏览器里管理数据库、存储位置、备份、归档、历史查询和结构变更。第一次打开时设置口令，之后凭口令进入。请只在本机访问。

## 开始使用

选一种方式启动，然后打开 http://127.0.0.1:8080 。第一次在页面上设置口令，之后用这个口令登录。再添加数据库和存储，然后创建备份、归档或结构变更。

### 下载二进制

从 [Releases](https://github.com/wwek/dmsx/releases) 下载和当前系统匹配的压缩包，解压后运行：

```bash
./dmsx
```

这种方式要本机已经装好对应数据库的客户端，备份和改表才会执行。

### 用容器

```bash
docker run -d --name dmsx \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e DMSX_ADDR=0.0.0.0:8080 \
  -v dmsx-data:/var/lib/dmsx \
  ghcr.io/wwek/dmsx:latest
```

镜像里带好常用数据库客户端。页面只映射到本机 8080。配置和任务记录保存在卷 `dmsx-data` 里。

## 技术清单

| 项目 | 范围 |
| --- | --- |
| 数据库 | MySQL、MariaDB、PostgreSQL（逻辑备份与物理备份）、MongoDB |
| 存储 | S3、七牛、本地目录、SFTP、FTP、Azure Blob、Google Drive、SMB、rclone |
| 可以做的事 | 定时备份、保留清理、恢复、单表归档、历史查询、在线改表 |
| 页面地址 | 默认 `127.0.0.1:8080`，用 `DMSX_ADDR` 修改 |
| 数据目录 | `DMSX_DATA`。二进制默认 `./data`，容器里是 `/var/lib/dmsx` |
| 登录 | 单人口令。哈希存在数据目录的库里，会话放在浏览器 cookie。口令不写进配置文件，也不用环境变量 |

二进制运行时，本机需要有 `mysqldump`、`mariadb-dump`、`pg_dump`、`pg_basebackup`、`mongodump`。`scripts/package.sh` 打包时会下载 [gh-ost](https://github.com/github/gh-ost) 的官方发布包，校验后编进 `dist/dmsx`，并在 `dist/gh-ost` 再放一份。`pt-online-schema-change` 仍要单独安装。可以用 `DMSX_MYSQLDUMP`、`DMSX_GH_OST` 这类变量指定路径。

## 核心技术清单

### 备份

导出工具的标准输出走同一条管道：`mysqldump` / `mariadb-dump` / `pg_dump` / `pg_basebackup` / `mongodump` → 明文 SQL 再 zstd → 可选 AES-GCM → 存储。加密文件头是 `DMSXBK01`。

S3 和七牛同一时刻只传一块 16 MiB，满 1000 块就换下一个对象，最后写 manifest。PostgreSQL 物理备份是 `pg_basebackup --pgdata=- --format=tar`。密码写在权限 `0600` 的客户端配置里，不放进进程参数。SSH 只做本地端口转发。从未跑过的计划会立刻视为到期。

这条路径按 [Databasus](https://github.com/databasus/databasus) 已公开的设计重写，没有搬它的源码。文件不能和 Databasus 的备份互换。说明见 [NOTICE.md](NOTICE.md)。进程边界见 [docs/architecture.md](docs/architecture.md)。

### 归档与历史查询

MySQL 游标按 `WHERE` 把行流进内存中的 Parquet，再上传为一个对象：

`dms_archives/<database>/<table>/<year>_archive.parquet`

回读对象、MD5 一致之后，才按每批 5000 行删除。同一年再次归档会另存一份。

历史查询用进程内的 DuckDB。S3 / 七牛通过 httpfs 读取，本地存储直接读目录。条件里出现 `;`、注释或 `read_` 会被拒绝，结果默认 `LIMIT 100`。

### 在线改表

dmsx 调用 `gh-ost` 或 `pt-online-schema-change`。影子表、分批拷贝、追 binlog 和表名互换由这些工具完成。dmsx 从标准输出解析进度和预计剩余时间，经 WebSocket 推到页面。未勾选「执行到切表」时不带 `--execute`。

## 开发

从源码启动和跑测试放在这里。日常使用走上面的二进制或容器。

```bash
go run ./cmd/dmsx
go test ./...
go test -tags e2e ./e2e
```

推送和 pull request 会跑这两条。

需要 Go 1.26。e2e 用 `e2e/docker-compose.yml` 拉起 MySQL 8.4，再做一次逻辑备份、带删除的归档，以及一次历史查询。

## 致谢

- https://github.com/databasus/databasus
- https://github.com/wal-g/wal-g
- https://github.com/gobackup/gobackup
