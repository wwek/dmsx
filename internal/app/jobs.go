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
	if err := a.validateConfig(ctx, &in); err != nil {
		return BackupConfig{}, err
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	in.Enabled = true
	in.FailCount = 0
	in.LastRunAt = nil
	if err := a.repo.Put(ctx, kindConfig, in.ID, in); err != nil {
		return BackupConfig{}, err
	}
	a.Audit(ctx, "backup_plan.create", in.Name+" for "+a.databaseName(ctx, in.DatabaseID))
	return in, nil
}

func (a *App) ListConfigs(ctx context.Context) ([]BackupConfig, error) {
	var rows []BackupConfig
	if err := a.repo.List(ctx, kindConfig, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []BackupConfig{}
	}
	return rows, nil
}

func (a *App) DeleteConfig(ctx context.Context, id string) error {
	var cfg BackupConfig
	if err := a.repo.Get(ctx, kindConfig, id, &cfg); err != nil {
		return err
	}
	if err := a.repo.Delete(ctx, kindConfig, id); err != nil {
		return err
	}
	a.Audit(ctx, "backup_plan.delete", cfg.Name)
	return nil
}

func (a *App) ListRuns(ctx context.Context) ([]BackupRun, error) {
	var rows []BackupRun
	if err := a.repo.List(ctx, kindRun, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []BackupRun{}
	}
	return rows, nil
}

// StartBackup is the operator's "back up now". forceFull starts a new physical
// full even when the plan is full_incr.
func (a *App) StartBackup(ctx context.Context, configID string, forceFull bool) (BackupRun, error) {
	run, err := a.startBackup(ctx, configID, forceFull)
	if err == nil {
		var cfg BackupConfig
		_ = a.repo.Get(ctx, kindConfig, configID, &cfg)
		detail := cfg.Name + " for " + a.databaseName(ctx, run.DatabaseID)
		if forceFull {
			detail += " (force full)"
		}
		a.Audit(ctx, "backup.run", detail)
	}
	return run, err
}

func (a *App) startBackup(ctx context.Context, configID string, forceFull bool) (BackupRun, error) {
	var cfg BackupConfig
	if err := a.repo.Get(ctx, kindConfig, configID, &cfg); err != nil {
		return BackupRun{}, err
	}
	run := BackupRun{
		ID:         uuid.NewString(),
		ConfigID:   cfg.ID,
		DatabaseID: cfg.DatabaseID,
		StorageID:  cfg.StorageID,
		Status:     "running",
		Encrypted:  cfg.Encrypted,
		ForceFull:  forceFull && cfg.Method == "physical",
		StartedAt:  time.Now().UTC(),
	}
	if err := a.repo.Put(ctx, kindRun, run.ID, run); err != nil {
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
		if cfg.Method == "physical" && cfg.WalEnabled {
			a.ensureWalStreamer(cfg)
		}
	}
	cfg.LastRunAt = &now
	var still BackupRun
	if err := a.repo.Get(context.WithoutCancel(ctx), kindRun, run.ID, &still); err != nil {
		return
	}
	_ = a.repo.Put(context.WithoutCancel(ctx), kindRun, run.ID, run)
	_ = a.repo.Put(context.WithoutCancel(ctx), kindConfig, cfg.ID, cfg)
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
	if cfg.Method == "physical" {
		cleanup, prepErr := a.preparePhysical(ctx, cfg, run, &src)
		defer cleanup()
		if prepErr != nil {
			return prepErr
		}
		_ = a.repo.Put(context.WithoutCancel(ctx), kindRun, run.ID, run)
	}
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
		_ = a.repo.Put(context.WithoutCancel(ctx), kindRun, run.ID, run)
	})
	if err != nil {
		return err
	}
	run.Bytes = result.Bytes
	run.SHA256 = result.SHA256
	run.Codec = result.Format.Name
	if cfg.Method == "physical" {
		if err := a.savePhysicalResult(ctx, *cfg, run, result, store); err != nil {
			return err
		}
	}
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
		if run.ManifestKey != "" {
			_ = store.Delete(ctx, run.ManifestKey)
		}
		_ = a.repo.Delete(ctx, kindRun, run.ID)
	}
}

func (a *App) OpenRun(ctx context.Context, id string) (io.ReadCloser, string, error) {
	var run BackupRun
	if err := a.repo.Get(ctx, kindRun, id, &run); err != nil {
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
	if _, row, err := a.store(ctx, in.StorageID); err != nil {
		return ArchiveJob{}, err
	} else if err := rejectUnreadStorage(row); err != nil {
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
	if err := a.repo.Put(ctx, kindArchive, in.ID, in); err != nil {
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
	_ = a.repo.Put(context.WithoutCancel(ctx), kindArchive, job.ID, job)
	if job.PlanID != "" {
		if plan, planErr := a.GetArchivePlan(context.WithoutCancel(ctx), job.PlanID); planErr == nil {
			plan.LastRunAt = &now
			_ = a.repo.Put(context.WithoutCancel(ctx), kindArchivePlan, plan.ID, plan)
		}
	}
	a.notifyArchive(job)
}

func (a *App) notifyArchive(job ArchiveJob) {
	name := a.databaseName(context.Background(), job.DatabaseID)
	text := fmt.Sprintf("表 %s\n条件 %s\n归档 %d 行，删除 %d 行", job.Table, job.Where, job.RowsArchived, job.RowsDeleted)
	if job.ObjectKey != "" {
		text += "\n对象 " + job.ObjectKey
	}
	if job.Status == "success" {
		a.notify(job.DatabaseID, notify.Message{Event: notify.EventArchiveSuccess, Heading: "归档成功：" + name, Text: text})
		return
	}
	text += "\n错误：" + clipText(job.Error, 1500)
	a.notify(job.DatabaseID, notify.Message{Event: notify.EventArchiveFailed, Heading: "归档失败：" + name, Text: text})
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
	table, err := archive.OpenMySQL(ctx, db.Username, db.Password, host, port, db.Database, job.Table, job.Where)
	if err != nil {
		return err
	}
	defer table.Close()
	dest, err := archiveDestination(store)
	if err != nil {
		return err
	}
	pause := time.Duration(job.PauseMS) * time.Millisecond
	var lastPersist time.Time
	result, err := archive.Run(ctx, archive.Request{
		Database: db.Database, Table: job.Table, Where: job.Where, Year: job.Year,
		BatchSize: job.BatchSize, DeleteAfter: job.DeleteAfter,
	}, table, dest, func() { time.Sleep(pause) }, func(p archive.Progress) {
		job.Phase = p.Phase
		job.RowsArchived = p.RowsArchived
		job.RowsDeleted = p.RowsDeleted
		if p.ObjectKey != "" {
			job.ObjectKey = p.ObjectKey
		}
		if p.MD5 != "" {
			job.MD5 = p.MD5
		}
		now := time.Now()
		if now.Sub(lastPersist) < 2*time.Second && p.Phase != archive.PhaseDone {
			return
		}
		lastPersist = now
		_ = a.repo.Put(context.WithoutCancel(ctx), kindArchive, job.ID, *job)
	})
	job.ObjectKey = result.Key
	if len(result.Keys) > 0 {
		job.ObjectKeys = result.Keys
		job.ObjectKey = result.Keys[len(result.Keys)-1]
	}
	job.MD5 = result.MD5
	job.RowsArchived = result.RowsArchived
	job.RowsDeleted = result.RowsDeleted
	if err == nil {
		job.Phase = archive.PhaseDone
	}
	return err
}

func rejectUnreadStorage(row Storage) error {
	if strings.EqualFold(row.Type, storage.TypeGoogleDrive) {
		return fmt.Errorf("table archive needs a storage that can be read back for MD5; Google Drive reads are not implemented")
	}
	return nil
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
	if err := a.repo.List(ctx, kindArchive, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ArchiveJob{}
	}
	return rows, nil
}

func (a *App) CreateArchivePlan(ctx context.Context, in ArchivePlan) (ArchivePlan, error) {
	db, err := a.database(ctx, in.DatabaseID)
	if err != nil {
		return ArchivePlan{}, err
	}
	if db.Engine != engine.MySQL && db.Engine != engine.MariaDB {
		return ArchivePlan{}, fmt.Errorf("table archive reads a MySQL or MariaDB cursor")
	}
	if _, row, err := a.store(ctx, in.StorageID); err != nil {
		return ArchivePlan{}, err
	} else if err := rejectUnreadStorage(row); err != nil {
		return ArchivePlan{}, err
	}
	if in.Name == "" {
		return ArchivePlan{}, fmt.Errorf("name is required")
	}
	if err := sqlident.ValidatePredicate(in.Where); err != nil {
		return ArchivePlan{}, err
	}
	year := in.Year
	if year == 0 {
		year = time.Now().UTC().Year()
	}
	if _, err := archive.ObjectKey(db.Database, in.Table, year, ""); err != nil {
		return ArchivePlan{}, err
	}
	if err := in.Schedule.Validate(); err != nil {
		return ArchivePlan{}, err
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 5000
	}
	if in.PauseMS <= 0 {
		in.PauseMS = 200
	}
	in.ID = uuid.NewString()
	in.Enabled = true
	in.CreatedAt = time.Now().UTC()
	if err := a.repo.Put(ctx, kindArchivePlan, in.ID, in); err != nil {
		return ArchivePlan{}, err
	}
	a.Audit(ctx, "archive.plan.create", fmt.Sprintf("%s → %s.%s", in.Name, db.Name, in.Table))
	return in, nil
}

func (a *App) ListArchivePlans(ctx context.Context) ([]ArchivePlan, error) {
	var rows []ArchivePlan
	if err := a.repo.List(ctx, kindArchivePlan, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ArchivePlan{}
	}
	return rows, nil
}

func (a *App) GetArchivePlan(ctx context.Context, id string) (ArchivePlan, error) {
	var plan ArchivePlan
	err := a.repo.Get(ctx, kindArchivePlan, id, &plan)
	return plan, err
}

func (a *App) DeleteArchivePlan(ctx context.Context, id string) error {
	plan, err := a.GetArchivePlan(ctx, id)
	if err != nil {
		return err
	}
	if err := a.repo.Delete(ctx, kindArchivePlan, id); err != nil {
		return err
	}
	a.Audit(ctx, "archive.plan.delete", plan.Name)
	return nil
}

// UpdateArchivePlan replaces schedule fields and the enabled flag. LastRunAt stays.
func (a *App) UpdateArchivePlan(ctx context.Context, id string, in ArchivePlan) (ArchivePlan, error) {
	existing, err := a.GetArchivePlan(ctx, id)
	if err != nil {
		return ArchivePlan{}, err
	}
	db, err := a.database(ctx, in.DatabaseID)
	if err != nil {
		return ArchivePlan{}, err
	}
	if db.Engine != engine.MySQL && db.Engine != engine.MariaDB {
		return ArchivePlan{}, fmt.Errorf("table archive reads a MySQL or MariaDB cursor")
	}
	if _, row, err := a.store(ctx, in.StorageID); err != nil {
		return ArchivePlan{}, err
	} else if err := rejectUnreadStorage(row); err != nil {
		return ArchivePlan{}, err
	}
	if in.Name == "" {
		return ArchivePlan{}, fmt.Errorf("name is required")
	}
	if err := sqlident.ValidatePredicate(in.Where); err != nil {
		return ArchivePlan{}, err
	}
	if err := in.Schedule.Validate(); err != nil {
		return ArchivePlan{}, err
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 5000
	}
	if in.PauseMS <= 0 {
		in.PauseMS = 200
	}
	in.ID = existing.ID
	in.CreatedAt = existing.CreatedAt
	in.LastRunAt = existing.LastRunAt
	if err := a.repo.Put(ctx, kindArchivePlan, in.ID, in); err != nil {
		return ArchivePlan{}, err
	}
	a.Audit(ctx, "archive.plan.update", in.Name)
	return in, nil
}

func (a *App) RunArchivePlan(ctx context.Context, id string) (ArchiveJob, error) {
	plan, err := a.GetArchivePlan(ctx, id)
	if err != nil {
		return ArchiveJob{}, err
	}
	year := plan.Year
	if year == 0 {
		year = time.Now().UTC().Year()
	}
	job, err := a.StartArchive(ctx, ArchiveJob{
		PlanID: plan.ID, DatabaseID: plan.DatabaseID, StorageID: plan.StorageID,
		Table: plan.Table, Where: plan.Where, Year: year,
		BatchSize: plan.BatchSize, PauseMS: plan.PauseMS, DeleteAfter: plan.DeleteAfter,
	})
	if err != nil {
		return ArchiveJob{}, err
	}
	a.Audit(ctx, "archive.plan.run", plan.Name)
	return job, nil
}

func (a *App) ColdQuery(ctx context.Context, in ColdRequest) ([]coldquery.Row, error) {
	db, err := a.database(ctx, in.DatabaseID)
	if err != nil {
		return nil, err
	}
	if db.Engine != engine.MySQL && db.Engine != engine.MariaDB {
		return nil, fmt.Errorf("cold query reads MySQL or MariaDB archives")
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
		req.SkipTLS = settings.SkipTLS
		req.Prefix = settings.Prefix
	} else {
		return nil, fmt.Errorf("cold query reads a filesystem, S3, Qiniu, OSS or GCS archive (%s is %s)", row.Name, row.Type)
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
	if err := a.repo.Put(ctx, kindDDL, in.ID, in); err != nil {
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
	var lastPersist time.Time
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
		now := time.Now()
		if now.Sub(lastPersist) < 2*time.Second {
			return
		}
		lastPersist = now
		_ = a.repo.Put(context.WithoutCancel(ctx), kindDDL, run.ID, run)
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
	_ = a.repo.Put(context.WithoutCancel(ctx), kindDDL, run.ID, run)
	payload, _ := json.Marshal(map[string]any{"kind": "done", "status": status, "error": run.Error})
	a.hub.Publish(run.ID, payload)
	a.notifyDDL(run)
}

func (a *App) notifyDDL(run DDLRun) {
	name := a.databaseName(context.Background(), run.DatabaseID)
	text := fmt.Sprintf("语句 %s\n工具 %s", run.Statement, run.Tool)
	if run.Percent > 0 {
		text += fmt.Sprintf("\n进度 %.1f%%", run.Percent)
	}
	if run.Status == "success" {
		a.notify(run.DatabaseID, notify.Message{Event: notify.EventDDLSuccess, Heading: "结构变更成功：" + name, Text: text})
		return
	}
	text += "\n错误：" + clipText(run.Error, 1500)
	a.notify(run.DatabaseID, notify.Message{Event: notify.EventDDLFailed, Heading: "结构变更失败：" + name, Text: text})
}

func (a *App) ListDDL(ctx context.Context) ([]DDLRun, error) {
	var rows []DDLRun
	if err := a.repo.List(ctx, kindDDL, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []DDLRun{}
	}
	return rows, nil
}

func (a *App) GetDDL(ctx context.Context, id string) (DDLRun, error) {
	var run DDLRun
	err := a.repo.Get(ctx, kindDDL, id, &run)
	return run, err
}

func (a *App) DDLEvents(id string) (<-chan []byte, func()) { return a.hub.Subscribe(id) }

func (a *App) DeleteRun(ctx context.Context, id string) error {
	var run BackupRun
	if err := a.repo.Get(ctx, kindRun, id, &run); err != nil {
		return err
	}
	if run.Status == "running" {
		return fmt.Errorf("backup is running")
	}
	if run.FileName != "" || run.ManifestKey != "" {
		if store, _, err := a.store(ctx, run.StorageID); err == nil {
			if run.FileName != "" {
				_ = store.Delete(ctx, run.FileName)
			}
			if run.ManifestKey != "" {
				_ = store.Delete(ctx, run.ManifestKey)
			}
		}
	}
	if err := a.repo.Delete(ctx, kindRun, id); err != nil {
		return err
	}
	a.Audit(ctx, "backup.delete", run.FileName)
	return nil
}

func (a *App) ListRestores(ctx context.Context) ([]RestoreRun, error) {
	var rows []RestoreRun
	if err := a.repo.List(ctx, kindRestore, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []RestoreRun{}
	}
	return rows, nil
}

func (a *App) StartRestore(ctx context.Context, backupID string, target RestoreTarget) (RestoreRun, error) {
	var run BackupRun
	if err := a.repo.Get(ctx, kindRun, backupID, &run); err != nil {
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
	clampRestoreParallel(run.Codec, &target)
	job := RestoreRun{
		ID:         uuid.NewString(),
		BackupID:   run.ID,
		DatabaseID: run.DatabaseID,
		Engine:     engineName,
		Database:   target.Database,
		Status:     "running",
		StartedAt:  time.Now().UTC(),
	}
	if err := a.repo.Put(ctx, kindRestore, job.ID, job); err != nil {
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
	report, err := a.restoreOnce(ctx, run, target)
	now := time.Now().UTC()
	job.FinishedAt = &now
	job.Report = report
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
	_ = a.repo.Put(context.WithoutCancel(ctx), kindRestore, job.ID, job)
	if cancelled {
		return
	}
	name := a.databaseName(context.WithoutCancel(ctx), run.DatabaseID)
	text := fmt.Sprintf("目标：%s:%d/%s\n备份：%s", target.Host, target.Port, target.Database, run.FileName)
	if report != "" {
		text += "\n校验：" + report
	}
	if job.Status == "success" {
		a.notify(run.DatabaseID, notify.Message{Event: notify.EventRestoreSuccess, Heading: "恢复成功：" + name, Text: text})
		return
	}
	a.notify(run.DatabaseID, notify.Message{Event: notify.EventRestoreFailed, Heading: "恢复失败：" + name, Text: text + "\n错误：" + clipText(job.Error, 1500)})
}

func (a *App) restoreOnce(ctx context.Context, run BackupRun, target RestoreTarget) (string, error) {
	body, _, err := a.OpenRun(ctx, run.ID)
	if err != nil {
		return "", err
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
		Parallel: target.Parallel,
	}
	format := engine.Format{Name: run.Codec, CompressInPipeline: run.Codec == "mysql-sql" || run.Codec == "mariadb-sql"}
	if err := engine.Restore(ctx, src, format, body); err != nil {
		return "", err
	}
	if !target.Verify {
		return "", nil
	}
	report, err := verifyRestore(ctx, src)
	return report, err
}

func clampRestoreParallel(codec string, target *RestoreTarget) {
	if codec != "pg-custom" {
		target.Parallel = 0
	}
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
	a.startEnabledWalStreamers(ctx)
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

func (a *App) startEnabledWalStreamers(ctx context.Context) {
	configs, err := a.ListConfigs(ctx)
	if err != nil {
		return
	}
	for _, cfg := range configs {
		if cfg.Enabled && cfg.Method == "physical" && cfg.WalEnabled {
			a.ensureWalStreamer(cfg)
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
		_, _ = a.startBackup(ctx, cfg.ID, false)
	}
	a.scheduleArchivePass(ctx, now)
}

func (a *App) scheduleArchivePass(ctx context.Context, now time.Time) {
	plans, err := a.ListArchivePlans(ctx)
	if err != nil {
		return
	}
	jobs, _ := a.ListArchives(ctx)
	running := map[string]bool{}
	for _, job := range jobs {
		if job.Status == "running" && job.PlanID != "" {
			running[job.PlanID] = true
		}
	}
	for _, plan := range plans {
		if !plan.Enabled || running[plan.ID] {
			continue
		}
		if !plan.Schedule.IsDue(now, plan.LastRunAt) {
			continue
		}
		year := plan.Year
		if year == 0 {
			year = now.Year()
		}
		_, _ = a.StartArchive(ctx, ArchiveJob{
			PlanID: plan.ID, DatabaseID: plan.DatabaseID, StorageID: plan.StorageID,
			Table: plan.Table, Where: plan.Where, Year: year,
			BatchSize: plan.BatchSize, PauseMS: plan.PauseMS, DeleteAfter: plan.DeleteAfter,
		})
	}
}
