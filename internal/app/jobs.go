package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/archive"
	"dmsx/internal/backup"
	"dmsx/internal/coldquery"
	"dmsx/internal/engine"
	"dmsx/internal/notify"
	"dmsx/internal/onlineddl"
	"dmsx/internal/retention"
	"dmsx/internal/sqlident"
	"dmsx/internal/storage"
)

func (a *App) CreateConfig(ctx context.Context, in BackupConfig) (BackupConfig, error) {
	if _, err := a.database(ctx, in.DatabaseID); err != nil {
		return BackupConfig{}, err
	}
	if _, _, err := a.store(ctx, in.StorageID); err != nil {
		return BackupConfig{}, err
	}
	if in.Method == "" {
		in.Method = "logical"
	}
	if in.Method != "logical" && in.Method != "physical" {
		return BackupConfig{}, fmt.Errorf("method must be logical or physical")
	}
	if err := in.Schedule.Validate(); err != nil {
		return BackupConfig{}, err
	}
	if err := in.Retention.Validate(); err != nil {
		return BackupConfig{}, err
	}
	for _, name := range append(append([]string{}, in.ExcludeTables...), in.ExcludeData...) {
		if !sqlident.Valid(name) {
			return BackupConfig{}, fmt.Errorf("invalid table name %q", name)
		}
	}
	if in.Name == "" {
		return BackupConfig{}, fmt.Errorf("name is required")
	}
	if in.MaxBackupSizeMB < 0 || in.MaxTotalSizeMB < 0 {
		return BackupConfig{}, fmt.Errorf("size limits cannot be negative")
	}
	if in.MaxFailedTries == 0 {
		in.MaxFailedTries = 3
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	in.Enabled = true
	if err := a.cat.Put(ctx, kindConfig, in.ID, in); err != nil {
		return BackupConfig{}, err
	}
	a.Audit(ctx, "backup_plan.create", in.Name+" for "+a.databaseName(ctx, in.DatabaseID))
	return in, nil
}

func (a *App) ListConfigs(ctx context.Context) ([]BackupConfig, error) {
	var rows []BackupConfig
	if err := a.cat.List(ctx, kindConfig, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []BackupConfig{}
	}
	return rows, nil
}

func (a *App) DeleteConfig(ctx context.Context, id string) error {
	var cfg BackupConfig
	if err := a.cat.Get(ctx, kindConfig, id, &cfg); err != nil {
		return err
	}
	if err := a.cat.Delete(ctx, kindConfig, id); err != nil {
		return err
	}
	a.Audit(ctx, "backup_plan.delete", cfg.Name)
	return nil
}

func (a *App) ListRuns(ctx context.Context) ([]BackupRun, error) {
	var rows []BackupRun
	if err := a.cat.List(ctx, kindRun, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []BackupRun{}
	}
	return rows, nil
}

// StartBackup is the operator's "back up now".
func (a *App) StartBackup(ctx context.Context, configID string) (BackupRun, error) {
	run, err := a.startBackup(ctx, configID)
	if err == nil {
		var cfg BackupConfig
		_ = a.cat.Get(ctx, kindConfig, configID, &cfg)
		a.Audit(ctx, "backup.run", cfg.Name+" for "+a.databaseName(ctx, run.DatabaseID))
	}
	return run, err
}

func (a *App) startBackup(ctx context.Context, configID string) (BackupRun, error) {
	var cfg BackupConfig
	if err := a.cat.Get(ctx, kindConfig, configID, &cfg); err != nil {
		return BackupRun{}, err
	}
	run := BackupRun{
		ID:         uuid.NewString(),
		ConfigID:   cfg.ID,
		DatabaseID: cfg.DatabaseID,
		StorageID:  cfg.StorageID,
		Status:     "running",
		Encrypted:  cfg.Encrypted,
		StartedAt:  time.Now().UTC(),
	}
	if err := a.cat.Put(ctx, kindRun, run.ID, run); err != nil {
		return BackupRun{}, err
	}
	jobCtx, cancel := context.WithCancel(a.root)
	a.track(run.ID, cancel)
	go a.executeBackup(jobCtx, cfg, run)
	return run, nil
}

func (a *App) executeBackup(ctx context.Context, cfg BackupConfig, run BackupRun) {
	defer a.untrack(run.ID)
	err := a.backupOnce(ctx, &cfg, &run)
	if err != nil && cfg.Method == "physical" && cfg.PhysicalCodec == "" && strings.Contains(err.Error(), "does not support compression") {
		cfg.PhysicalCodec = "gzip"
		run.FileName = ""
		err = a.backupOnce(ctx, &cfg, &run)
	}
	now := time.Now().UTC()
	run.FinishedAt = &now
	cancelled := ctx.Err() != nil
	if err != nil {
		run.Status = "failed"
		if cancelled {
			run.Error = "cancelled"
		} else {
			run.Error = err.Error()
		}
		cfg.FailCount++
	} else {
		run.Status = "success"
		run.Error = ""
		cfg.FailCount = 0
		a.applyRetention(ctx, cfg)
	}
	cfg.LastRunAt = &now
	var still BackupRun
	if err := a.cat.Get(context.WithoutCancel(ctx), kindRun, run.ID, &still); err != nil {
		return
	}
	_ = a.cat.Put(context.WithoutCancel(ctx), kindRun, run.ID, run)
	_ = a.cat.Put(context.WithoutCancel(ctx), kindConfig, cfg.ID, cfg)
	if !cancelled {
		a.notifyBackup(context.WithoutCancel(ctx), cfg, run)
	}
}

func (a *App) notifyBackup(ctx context.Context, cfg BackupConfig, run BackupRun) {
	name := a.databaseName(ctx, cfg.DatabaseID)
	took := ""
	if run.FinishedAt != nil {
		took = run.FinishedAt.Sub(run.StartedAt).Round(time.Second).String()
	}
	if run.Status == "success" {
		a.notify(cfg.DatabaseID, notify.Message{
			Event:   notify.EventBackupSuccess,
			Heading: "备份成功：" + name,
			Text:    fmt.Sprintf("计划：%s\n大小：%s\n用时：%s\n文件：%s", cfg.Name, formatBytes(run.Bytes), took, run.FileName),
		})
		return
	}
	text := fmt.Sprintf("计划：%s\n用时：%s\n错误：%s", cfg.Name, took, clipText(run.Error, 1500))
	if cfg.Retry && cfg.FailCount < cfg.MaxFailedTries {
		text += fmt.Sprintf("\n稍后重试（已失败 %d 次，上限 %d 次）", cfg.FailCount, cfg.MaxFailedTries)
	}
	a.notify(cfg.DatabaseID, notify.Message{Event: notify.EventBackupFailed, Heading: "备份失败：" + name, Text: text})
}

func (a *App) backupOnce(ctx context.Context, cfg *BackupConfig, run *BackupRun) error {
	db, err := a.database(ctx, cfg.DatabaseID)
	if err != nil {
		return err
	}
	store, _, err := a.store(ctx, cfg.StorageID)
	if err != nil {
		return err
	}
	src, release, err := a.source(ctx, db, cfg.Method == "physical")
	if err != nil {
		return err
	}
	defer release()
	if cfg.Method == "physical" && src.Engine != engine.PostgreSQL {
		return fmt.Errorf("physical backup is the PostgreSQL base-backup stream")
	}
	src.PhysicalCodec = cfg.PhysicalCodec
	src.ExcludeTables = cfg.ExcludeTables
	src.IncludeSchemas = cfg.IncludeSchemas
	src.ExcludeTableData = cfg.ExcludeData
	fileID, err := uuid.Parse(run.ID)
	if err != nil {
		return err
	}
	ext := engineExtension(src)
	if cfg.Encrypted {
		ext += ".enc"
	}
	run.FileName = fmt.Sprintf("backups/%s/%s_%s%s", db.ID, run.StartedAt.UTC().Format("20060102T150405Z"), run.ID, ext)
	result, err := backup.Run(ctx, src, cfg.Encrypted, a.master, fileID, store, run.FileName, cfg.MaxBackupSizeMB<<20, func(n int64) {
		run.Bytes = n
		_ = a.cat.Put(context.WithoutCancel(ctx), kindRun, run.ID, run)
	})
	if err != nil {
		return err
	}
	run.Bytes = result.Bytes
	run.SHA256 = result.SHA256
	run.Codec = result.Format.Name
	return nil
}

func engineExtension(src engine.Source) string {
	switch src.Engine {
	case engine.MySQL, engine.MariaDB:
		return ".sql.zst"
	case engine.MongoDB:
		return ".archive.gz"
	default:
		if src.Physical {
			return ".tar"
		}
		return ".dump"
	}
}

func (a *App) applyRetention(ctx context.Context, cfg BackupConfig) {
	runs, err := a.ListRuns(ctx)
	if err != nil {
		return
	}
	var items []retention.Item
	byID := map[string]BackupRun{}
	for _, run := range runs {
		if run.ConfigID != cfg.ID || run.Status != "success" {
			continue
		}
		id, err := uuid.Parse(run.ID)
		if err != nil {
			continue
		}
		items = append(items, retention.Item{ID: id, CreatedAt: run.StartedAt, Bytes: run.Bytes})
		byID[run.ID] = run
	}
	store, _, err := a.store(ctx, cfg.StorageID)
	if err != nil {
		return
	}
	expired := map[uuid.UUID]bool{}
	for _, id := range cfg.Retention.Expired(time.Now().UTC(), items) {
		expired[id] = true
	}
	var kept []retention.Item
	for _, item := range items {
		if !expired[item.ID] {
			kept = append(kept, item)
		}
	}
	for _, id := range retention.OverTotal(cfg.MaxTotalSizeMB<<20, kept) {
		expired[id] = true
	}
	for id := range expired {
		run := byID[id.String()]
		_ = store.Delete(ctx, run.FileName)
		_ = a.cat.Delete(ctx, kindRun, run.ID)
	}
}

func (a *App) OpenRun(ctx context.Context, id string) (io.ReadCloser, string, error) {
	var run BackupRun
	if err := a.cat.Get(ctx, kindRun, id, &run); err != nil {
		return nil, "", err
	}
	if run.Status != "success" {
		return nil, "", fmt.Errorf("run is %s", run.Status)
	}
	store, _, err := a.store(ctx, run.StorageID)
	if err != nil {
		return nil, "", err
	}
	body, err := store.Open(ctx, run.FileName)
	if err != nil {
		return nil, "", err
	}
	format := engine.Format{Name: run.Codec, CompressInPipeline: run.Codec == "mysql-sql" || run.Codec == "mariadb-sql"}
	plain, err := backup.OpenStored(a.master, format, run.Encrypted, body)
	if err != nil {
		body.Close()
		return nil, "", err
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: plain, Closer: body}, filepath.Base(run.FileName), nil
}

func (a *App) StartArchive(ctx context.Context, in ArchiveJob) (ArchiveJob, error) {
	if _, err := a.database(ctx, in.DatabaseID); err != nil {
		return ArchiveJob{}, err
	}
	if _, _, err := a.store(ctx, in.StorageID); err != nil {
		return ArchiveJob{}, err
	}
	db, _ := a.database(ctx, in.DatabaseID)
	if db.Engine != engine.MySQL && db.Engine != engine.MariaDB {
		return ArchiveJob{}, fmt.Errorf("table archive reads a MySQL or MariaDB cursor")
	}
	if err := sqlident.ValidatePredicate(in.Where); err != nil {
		return ArchiveJob{}, err
	}
	if _, err := archive.ObjectKey(db.Database, in.Table, in.Year, ""); err != nil {
		return ArchiveJob{}, err
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 5000
	}
	if in.PauseMS <= 0 {
		in.PauseMS = 200
	}
	in.ID = uuid.NewString()
	in.Status = "running"
	in.StartedAt = time.Now().UTC()
	if err := a.cat.Put(ctx, kindArchive, in.ID, in); err != nil {
		return ArchiveJob{}, err
	}
	detail := fmt.Sprintf("%s.%s WHERE %s", db.Name, in.Table, in.Where)
	if in.DeleteAfter {
		detail += " (delete after archive)"
	}
	a.Audit(ctx, "archive.start", detail)
	jobCtx, cancel := context.WithCancel(a.root)
	a.track(in.ID, cancel)
	go a.executeArchive(jobCtx, in)
	return in, nil
}

func (a *App) executeArchive(ctx context.Context, job ArchiveJob) {
	defer a.untrack(job.ID)
	err := a.archiveOnce(ctx, &job)
	now := time.Now().UTC()
	job.FinishedAt = &now
	if err != nil {
		job.Status = "failed"
		if ctx.Err() != nil {
			job.Error = "cancelled"
		} else {
			job.Error = err.Error()
		}
	} else {
		job.Status = "success"
	}
	_ = a.cat.Put(context.WithoutCancel(ctx), kindArchive, job.ID, job)
}

func (a *App) archiveOnce(ctx context.Context, job *ArchiveJob) error {
	db, err := a.database(ctx, job.DatabaseID)
	if err != nil {
		return err
	}
	store, _, err := a.store(ctx, job.StorageID)
	if err != nil {
		return err
	}
	host, port := db.Host, db.Port
	if db.SSH != nil && db.SSH.Host != "" {
		tunnelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		addr, err := dialSSH(tunnelCtx, *db.SSH, db.Host, db.Port)
		if err != nil {
			return err
		}
		host, port, err = splitAddr(addr)
		if err != nil {
			return err
		}
	}
	cursor, err := archive.OpenMySQL(ctx, db.Username, db.Password, host, port, db.Database, job.Table, job.Where)
	if err != nil {
		return err
	}
	defer cursor.Close()
	dest, err := archiveDestination(store)
	if err != nil {
		return err
	}
	var deleter archive.Deleter
	if job.DeleteAfter {
		deleter = archive.MySQLDeleter{
			User: db.Username, Password: db.Password, Host: host, Port: port,
			Database: db.Database, Table: job.Table, Where: job.Where,
		}
	}
	pause := time.Duration(job.PauseMS) * time.Millisecond
	result, err := archive.Run(ctx, archive.Request{
		Database: db.Database, Table: job.Table, Where: job.Where, Year: job.Year,
		BatchSize: job.BatchSize, DeleteAfter: job.DeleteAfter,
	}, cursor, dest, deleter, func() { time.Sleep(pause) })
	job.ObjectKey = result.Key
	job.MD5 = result.MD5
	job.RowsArchived = result.RowsArchived
	job.RowsDeleted = result.RowsDeleted
	return err
}

func archiveDestination(store storage.Store) (archive.Destination, error) {
	if settings, ok := storage.AsS3(store); ok {
		return archive.NewS3Uploader(settings)
	}
	return storeDest{store}, nil
}

type storeDest struct{ store storage.Store }

func (d storeDest) Upload(ctx context.Context, key string, r io.Reader) error {
	return d.store.Put(ctx, key, r)
}
func (d storeDest) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return d.store.Open(ctx, key)
}
func (d storeDest) Exists(ctx context.Context, key string) (bool, error) {
	return d.store.Exists(ctx, key)
}

func (a *App) ListArchives(ctx context.Context) ([]ArchiveJob, error) {
	var rows []ArchiveJob
	if err := a.cat.List(ctx, kindArchive, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ArchiveJob{}
	}
	return rows, nil
}

func (a *App) ColdQuery(ctx context.Context, in ColdRequest) ([]coldquery.Row, error) {
	db, err := a.database(ctx, in.DatabaseID)
	if err != nil {
		return nil, err
	}
	store, row, err := a.store(ctx, in.StorageID)
	if err != nil {
		return nil, err
	}
	req := coldquery.Request{Database: db.Database, Table: in.Table, Where: in.Where, Limit: in.Limit}
	if root, ok := storage.AsLocal(store); ok {
		req.LocalGlob = filepath.Join(root, "dms_archives", db.Database, in.Table, "*.parquet")
	} else if settings, ok := storage.AsS3(store); ok {
		req.Bucket = settings.Bucket
		req.Region = settings.Region
		req.Endpoint = settings.Endpoint
		req.AccessKey = settings.AccessKey
		req.SecretKey = settings.SecretKey
		req.PathStyle = !settings.VirtualHost
		req.UseSSL = !strings.HasPrefix(settings.Endpoint, "http://")
	} else {
		return nil, fmt.Errorf("cold query reads a local directory or an S3/Qiniu bucket (%s is %s)", row.Name, row.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return coldquery.Query(ctx, req)
}

func (a *App) StartDDL(ctx context.Context, in DDLRun) (DDLRun, error) {
	db, err := a.database(ctx, in.DatabaseID)
	if err != nil {
		return DDLRun{}, err
	}
	if db.Engine != engine.MySQL && db.Engine != engine.MariaDB {
		return DDLRun{}, fmt.Errorf("online DDL runs against MySQL or MariaDB")
	}
	database, table, body, err := onlineddl.ParseAlter(in.Statement)
	if err != nil {
		return DDLRun{}, err
	}
	if database == "" {
		database = db.Database
	}
	in.Table = table
	in.Alter = body
	in.Statement = strings.TrimSpace(in.Statement)
	if in.Tool == "" {
		in.Tool = "gh-ost"
	}
	if in.ChunkSize <= 0 {
		in.ChunkSize = 1000
	}
	in.ID = uuid.NewString()
	in.Status = "running"
	in.StartedAt = time.Now().UTC()
	if err := a.cat.Put(ctx, kindDDL, in.ID, in); err != nil {
		return DDLRun{}, err
	}
	mode := "dry run"
	if in.Execute {
		mode = "execute"
	}
	a.Audit(ctx, "ddl.start", fmt.Sprintf("%s via %s (%s): %s", db.Name, in.Tool, mode, in.Statement))
	jobCtx, cancel := context.WithCancel(a.root)
	a.track(in.ID, cancel)
	go a.executeDDL(jobCtx, db, in, database)
	return in, nil
}

func (a *App) executeDDL(ctx context.Context, db Database, run DDLRun, database string) {
	defer a.untrack(run.ID)
	host, port := db.Host, db.Port
	var release func()
	if db.SSH != nil && db.SSH.Host != "" {
		tunnelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		addr, err := dialSSH(tunnelCtx, *db.SSH, db.Host, db.Port)
		if err != nil {
			a.finishDDL(ctx, run, err)
			return
		}
		host, port, err = splitAddr(addr)
		if err != nil {
			a.finishDDL(ctx, run, err)
			return
		}
		release = cancel
		_ = release
	}
	var log strings.Builder
	err := onlineddl.Run(ctx, onlineddl.Request{
		Host: host, Port: port, User: db.Username, Password: db.Password,
		Database: database, Statement: run.Statement, Tool: run.Tool,
		Execute: run.Execute, ChunkSize: run.ChunkSize,
	}, func(line string) {
		if log.Len() < 64<<10 {
			log.WriteString(line)
			log.WriteByte('\n')
		}
		run.Log = log.String()
		event := map[string]any{"kind": "log", "line": line}
		if progress, ok := onlineddl.ParseLine(line); ok {
			run.Copied = progress.Copied
			run.Total = progress.Total
			run.Percent = progress.Percent
			run.ETA = progress.ETA
			event = map[string]any{"kind": "progress", "line": line, "progress": progress}
		}
		payload, _ := json.Marshal(event)
		a.hub.Publish(run.ID, payload)
		_ = a.cat.Put(context.WithoutCancel(ctx), kindDDL, run.ID, run)
	})
	a.finishDDL(ctx, run, err)
}

func (a *App) finishDDL(ctx context.Context, run DDLRun, err error) {
	now := time.Now().UTC()
	run.FinishedAt = &now
	status := "success"
	if err != nil {
		status = "failed"
		if ctx.Err() != nil {
			run.Error = "cancelled"
		} else {
			run.Error = err.Error()
		}
	}
	run.Status = status
	_ = a.cat.Put(context.WithoutCancel(ctx), kindDDL, run.ID, run)
	payload, _ := json.Marshal(map[string]any{"kind": "done", "status": status, "error": run.Error})
	a.hub.Publish(run.ID, payload)
}

func (a *App) ListDDL(ctx context.Context) ([]DDLRun, error) {
	var rows []DDLRun
	if err := a.cat.List(ctx, kindDDL, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []DDLRun{}
	}
	return rows, nil
}

func (a *App) GetDDL(ctx context.Context, id string) (DDLRun, error) {
	var run DDLRun
	err := a.cat.Get(ctx, kindDDL, id, &run)
	return run, err
}

func (a *App) DDLEvents(id string) (<-chan []byte, func()) { return a.hub.Subscribe(id) }

func (a *App) DeleteRun(ctx context.Context, id string) error {
	var run BackupRun
	if err := a.cat.Get(ctx, kindRun, id, &run); err != nil {
		return err
	}
	if run.Status == "running" {
		return fmt.Errorf("backup is running")
	}
	if run.FileName != "" {
		if store, _, err := a.store(ctx, run.StorageID); err == nil {
			_ = store.Delete(ctx, run.FileName)
		}
	}
	if err := a.cat.Delete(ctx, kindRun, id); err != nil {
		return err
	}
	a.Audit(ctx, "backup.delete", run.FileName)
	return nil
}

func (a *App) ListRestores(ctx context.Context) ([]RestoreRun, error) {
	var rows []RestoreRun
	if err := a.cat.List(ctx, kindRestore, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []RestoreRun{}
	}
	return rows, nil
}

func (a *App) StartRestore(ctx context.Context, backupID string, target RestoreTarget) (RestoreRun, error) {
	var run BackupRun
	if err := a.cat.Get(ctx, kindRun, backupID, &run); err != nil {
		return RestoreRun{}, err
	}
	if run.Status != "success" {
		return RestoreRun{}, fmt.Errorf("run is %s", run.Status)
	}
	engineName, err := restoreEngine(run.Codec)
	if err != nil {
		return RestoreRun{}, err
	}
	if target.Host == "" || target.Port == 0 || target.Username == "" || target.Database == "" {
		return RestoreRun{}, fmt.Errorf("restore target needs host, port, username and database")
	}
	job := RestoreRun{
		ID:         uuid.NewString(),
		BackupID:   run.ID,
		DatabaseID: run.DatabaseID,
		Engine:     engineName,
		Database:   target.Database,
		Status:     "running",
		StartedAt:  time.Now().UTC(),
	}
	if err := a.cat.Put(ctx, kindRestore, job.ID, job); err != nil {
		return RestoreRun{}, err
	}
	a.Audit(ctx, "restore.start", fmt.Sprintf("%s → %s:%d/%s", run.FileName, target.Host, target.Port, target.Database))
	jobCtx, cancel := context.WithCancel(a.root)
	a.track(job.ID, cancel)
	go a.executeRestore(jobCtx, run, job, target)
	return job, nil
}

func (a *App) executeRestore(ctx context.Context, run BackupRun, job RestoreRun, target RestoreTarget) {
	defer a.untrack(job.ID)
	err := a.restoreOnce(ctx, run, target)
	now := time.Now().UTC()
	job.FinishedAt = &now
	cancelled := ctx.Err() != nil
	if err != nil {
		job.Status = "failed"
		if cancelled {
			job.Error = "cancelled"
		} else {
			job.Error = err.Error()
		}
	} else {
		job.Status = "success"
	}
	_ = a.cat.Put(context.WithoutCancel(ctx), kindRestore, job.ID, job)
	if cancelled {
		return
	}
	name := a.databaseName(context.WithoutCancel(ctx), run.DatabaseID)
	text := fmt.Sprintf("目标：%s:%d/%s\n备份：%s", target.Host, target.Port, target.Database, run.FileName)
	if job.Status == "success" {
		a.notify(run.DatabaseID, notify.Message{Event: notify.EventRestoreSuccess, Heading: "恢复成功：" + name, Text: text})
		return
	}
	a.notify(run.DatabaseID, notify.Message{Event: notify.EventRestoreFailed, Heading: "恢复失败：" + name, Text: text + "\n错误：" + clipText(job.Error, 1500)})
}

func (a *App) restoreOnce(ctx context.Context, run BackupRun, target RestoreTarget) error {
	body, _, err := a.OpenRun(ctx, run.ID)
	if err != nil {
		return err
	}
	defer body.Close()
	src := engine.Source{
		Engine:   restoreEngineName(run.Codec),
		Host:     target.Host,
		Port:     target.Port,
		User:     target.Username,
		Password: target.Password,
		Database: target.Database,
		SSL:      target.SSL,
	}
	format := engine.Format{Name: run.Codec, CompressInPipeline: run.Codec == "mysql-sql" || run.Codec == "mariadb-sql"}
	return engine.Restore(ctx, src, format, body)
}

func restoreEngine(codec string) (string, error) {
	switch codec {
	case "mysql-sql", "mariadb-sql", "pg-custom", "mongo-archive":
		return restoreEngineName(codec), nil
	default:
		if strings.HasPrefix(codec, "pg-base") {
			return "", fmt.Errorf("a physical base backup is restored from the downloaded tar, not piped into a live server")
		}
		return "", fmt.Errorf("format %q cannot be restored", codec)
	}
}

func restoreEngineName(codec string) string {
	switch codec {
	case "mysql-sql":
		return engine.MySQL
	case "mariadb-sql":
		return engine.MariaDB
	case "mongo-archive":
		return engine.MongoDB
	default:
		return engine.PostgreSQL
	}
}

func (a *App) Schedule(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.schedulePass(ctx)
		}
	}
}

func (a *App) schedulePass(ctx context.Context) {
	configs, err := a.ListConfigs(ctx)
	if err != nil {
		return
	}
	runs, _ := a.ListRuns(ctx)
	running := map[string]bool{}
	for _, run := range runs {
		if run.Status == "running" {
			running[run.ConfigID] = true
		}
	}
	now := time.Now().UTC()
	for _, cfg := range configs {
		if !cfg.Enabled || running[cfg.ID] {
			continue
		}
		due := cfg.Schedule.IsDue(now, cfg.LastRunAt)
		retry := cfg.Retry && cfg.FailCount > 0 && cfg.FailCount < cfg.MaxFailedTries && cfg.LastRunAt != nil && now.Sub(*cfg.LastRunAt) >= 2*time.Minute
		if !due && !retry {
			continue
		}
		_, _ = a.startBackup(ctx, cfg.ID)
	}
}
