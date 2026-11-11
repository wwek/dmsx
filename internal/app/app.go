package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/engine"
	"dmsx/internal/repo"
	"dmsx/internal/secret"
	"dmsx/internal/sqlident"
	"dmsx/internal/storage"
)

type App struct {
	repo    *repo.Repo
	box     *secret.Box
	master  string
	dataDir string
	root    context.Context
	cancel  context.CancelFunc
	hub     *hub

	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	// healthMu serializes read-modify-write of database rows and probe state.
	healthMu sync.Mutex
}

func Open(dataDir string) (*App, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dataDir, "master.key")
	box, err := secret.Load(keyPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	repoStore, err := repo.Open(filepath.Join(dataDir, "dmsx.db"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	application := &App{
		repo:    repoStore,
		box:     box,
		master:  strings.TrimSpace(string(raw)),
		dataDir: dataDir,
		root:    ctx,
		cancel:  cancel,
		hub:     newHub(),
		cancels: map[string]context.CancelFunc{},
	}
	application.recoverInterrupted()
	return application, nil
}

func (a *App) Close() error {
	a.cancel()
	return a.repo.Close()
}

func (a *App) recoverInterrupted() {
	now := time.Now().UTC()
	var runs []BackupRun
	_ = a.repo.List(context.Background(), kindRun, &runs)
	for _, run := range runs {
		if run.Status == "running" {
			run.Status = "failed"
			run.Error = "dmsx stopped before the run finished"
			run.FinishedAt = &now
			_ = a.repo.Put(context.Background(), kindRun, run.ID, run)
		}
	}
	var jobs []ArchiveJob
	_ = a.repo.List(context.Background(), kindArchive, &jobs)
	for _, job := range jobs {
		if job.Status == "running" {
			job.Status = "failed"
			job.Error = "dmsx stopped before the archive finished"
			job.FinishedAt = &now
			_ = a.repo.Put(context.Background(), kindArchive, job.ID, job)
		}
	}
	var ddls []DDLRun
	_ = a.repo.List(context.Background(), kindDDL, &ddls)
	for _, run := range ddls {
		if run.Status == "running" {
			run.Status = "failed"
			run.Error = "dmsx stopped before the DDL finished"
			run.FinishedAt = &now
			_ = a.repo.Put(context.Background(), kindDDL, run.ID, run)
		}
	}
	var restores []RestoreRun
	_ = a.repo.List(context.Background(), kindRestore, &restores)
	for _, run := range restores {
		if run.Status == "running" {
			run.Status = "failed"
			run.Error = "dmsx stopped before the restore finished"
			run.FinishedAt = &now
			_ = a.repo.Put(context.Background(), kindRestore, run.ID, run)
		}
	}
}

func (a *App) CreateDatabase(ctx context.Context, in Database) (Database, error) {
	if err := checkEngine(in.Engine); err != nil {
		return Database{}, err
	}
	if in.Name == "" || in.Host == "" || in.Username == "" || in.Database == "" || in.Port <= 0 {
		return Database{}, fmt.Errorf("name, host, port, username and database are required")
	}
	if !sqlident.Valid(in.Database) {
		return Database{}, fmt.Errorf("database name must be a plain identifier")
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	in.HasPassword = in.Password != ""
	if err := a.sealDatabase(&in); err != nil {
		return Database{}, err
	}
	if in.HealthCheck != nil {
		if err := in.HealthCheck.Validate(); err != nil {
			return Database{}, err
		}
	}
	if err := a.repo.Put(ctx, kindDatabase, in.ID, in); err != nil {
		return Database{}, err
	}
	a.Audit(ctx, "database.create", fmt.Sprintf("%s (%s %s:%d/%s)", in.Name, in.Engine, in.Host, in.Port, in.Database))
	return in.Public(), nil
}

// CreateBackupUser creates a login on the server that can back up and cannot
// write, and switches the stored connection to it. The new password is sealed
// in the metadata repo and is not returned.
func (a *App) CreateBackupUser(ctx context.Context, id string, replication bool) (Database, error) {
	db, err := a.database(ctx, id)
	if err != nil {
		return Database{}, err
	}
	if replication && db.Engine != engine.PostgreSQL {
		return Database{}, fmt.Errorf("a replication user is for PostgreSQL physical backup")
	}
	// MySQL commits CREATE USER and each GRANT on its own, so a dropped request
	// must not cut the creation in half.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	src, release, err := a.source(ctx, db, false)
	if err != nil {
		return Database{}, err
	}
	defer release()
	user, err := engine.CreateBackupUser(ctx, src, replication)
	if err != nil {
		return Database{}, err
	}
	sealed, err := a.box.Seal(user.Password)
	if err != nil {
		return Database{}, err
	}
	a.healthMu.Lock()
	err = a.repo.Get(ctx, kindDatabase, id, &db)
	if err == nil {
		db.Username = user.Username
		db.Password = sealed
		db.HasPassword = true
		err = a.repo.Put(ctx, kindDatabase, db.ID, db)
	}
	a.healthMu.Unlock()
	if err != nil {
		return Database{}, fmt.Errorf("user %s was created but the connection was not saved: %w", user.Username, err)
	}
	kind := "read-only"
	if replication {
		kind = "replication"
	}
	a.Audit(ctx, "database.backup_user", fmt.Sprintf("%s: %s user %s", db.Name, kind, user.Username))
	return db.Public(), nil
}

func (a *App) ListDatabases(ctx context.Context) ([]Database, error) {
	var rows []Database
	if err := a.repo.List(ctx, kindDatabase, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Database{}
	}
	for i := range rows {
		rows[i] = rows[i].Public()
	}
	return rows, nil
}

func (a *App) DeleteDatabase(ctx context.Context, id string) error {
	runs, err := a.ListRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.DatabaseID != id {
			continue
		}
		if run.Status == "running" {
			a.Cancel(run.ID)
			run.Status = "cancelled"
			if err := a.repo.Put(ctx, kindRun, run.ID, run); err != nil {
				return err
			}
		}
		if err := a.DeleteRun(ctx, run.ID); err != nil && !strings.Contains(err.Error(), "not found") {
			return err
		}
	}
	configs, err := a.ListConfigs(ctx)
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		if cfg.DatabaseID == id {
			if err := a.repo.Delete(ctx, kindConfig, cfg.ID); err != nil {
				return err
			}
		}
	}
	plans, err := a.ListArchivePlans(ctx)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if plan.DatabaseID == id {
			if err := a.repo.Delete(ctx, kindArchivePlan, plan.ID); err != nil {
				return err
			}
		}
	}
	jobs, err := a.ListArchives(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.DatabaseID != id {
			continue
		}
		if err := a.deleteObjects(ctx, job.StorageID, append(job.ObjectKeys, job.ObjectKey)...); err != nil {
			return err
		}
		if err := a.repo.Delete(ctx, kindArchive, job.ID); err != nil {
			return err
		}
	}
	segments, err := a.ListWalSegments(ctx, id)
	if err != nil {
		return err
	}
	for _, segment := range segments {
		if err := a.deleteObjects(ctx, segment.StorageID, segment.ObjectKey); err != nil {
			return err
		}
		if err := a.repo.Delete(ctx, kindWalSegment, segment.ID); err != nil {
			return err
		}
	}
	if err := a.repo.Delete(ctx, kindHealth, id); err != nil {
		return err
	}
	name := a.databaseName(ctx, id)
	if err := a.repo.Delete(ctx, kindDatabase, id); err != nil {
		return err
	}
	a.Audit(ctx, "database.delete", name)
	return nil
}

func (a *App) TestDatabase(ctx context.Context, id string) error {
	db, err := a.database(ctx, id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	src, release, err := a.source(ctx, db, false)
	if err != nil {
		return err
	}
	defer release()
	return engine.Ping(ctx, src)
}

func (a *App) CreateStorage(ctx context.Context, in Storage) (Storage, error) {
	if _, err := storage.New(in.Type, in.Settings); err != nil {
		return Storage{}, err
	}
	if in.Name == "" {
		return Storage{}, fmt.Errorf("name is required")
	}
	sealed, err := a.sealSettings(in.Settings, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	in.Settings = sealed
	if err := a.repo.Put(ctx, kindStorage, in.ID, in); err != nil {
		return Storage{}, err
	}
	a.Audit(ctx, "storage.create", in.Name+" ("+in.Type+")")
	opened, err := a.openSettings(sealed, storageSecrets)
	if err != nil {
		return Storage{}, err
	}
	in.Settings = maskSettings(opened, storageSecrets)
	return in, nil
}

func (a *App) ListStorages(ctx context.Context) ([]Storage, error) {
	var rows []Storage
	if err := a.repo.List(ctx, kindStorage, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Storage{}
	}
	for i := range rows {
		opened, err := a.openSettings(rows[i].Settings, storageSecrets)
		if err != nil {
			return nil, err
		}
		rows[i].Settings = maskSettings(opened, storageSecrets)
	}
	return rows, nil
}

func (a *App) DeleteStorage(ctx context.Context, id string) error {
	var row Storage
	if err := a.repo.Get(ctx, kindStorage, id, &row); err != nil {
		return err
	}
	if err := a.repo.Delete(ctx, kindStorage, id); err != nil {
		return err
	}
	a.Audit(ctx, "storage.delete", row.Name)
	return nil
}

func (a *App) TestStorage(ctx context.Context, id string) error {
	store, _, err := a.store(ctx, id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return store.Test(ctx)
}

// ProbeStorage checks a destination from the form before it is saved.
// When id is set, a blank secret keeps the value already stored.
func (a *App) ProbeStorage(ctx context.Context, in Storage) error {
	settings := in.Settings
	kind := in.Type
	if in.ID != "" {
		var existing Storage
		if err := a.repo.Get(ctx, kindStorage, in.ID, &existing); err != nil {
			return err
		}
		if kind == "" {
			kind = existing.Type
		}
		opened, err := a.openSettings(existing.Settings, storageSecrets)
		if err != nil {
			return err
		}
		settings, err = mergeSettings(opened, in.Settings, storageSecrets)
		if err != nil {
			return err
		}
	}
	store, err := storage.New(kind, settings)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return store.Test(ctx)
}

func (a *App) database(ctx context.Context, id string) (Database, error) {
	var row Database
	if err := a.repo.Get(ctx, kindDatabase, id, &row); err != nil {
		return Database{}, err
	}
	if err := a.openDatabase(&row); err != nil {
		return Database{}, err
	}
	return row, nil
}

func (a *App) store(ctx context.Context, id string) (storage.Store, Storage, error) {
	var row Storage
	if err := a.repo.Get(ctx, kindStorage, id, &row); err != nil {
		return nil, Storage{}, err
	}
	opened, err := a.openSettings(row.Settings, storageSecrets)
	if err != nil {
		return nil, Storage{}, err
	}
	row.Settings = opened
	store, err := storage.New(row.Type, opened)
	return store, row, err
}

func (a *App) source(ctx context.Context, db Database, physical bool) (engine.Source, func(), error) {
	host, port := db.Host, db.Port
	release := func() {}
	if db.SSH != nil && db.SSH.Host != "" {
		tunnelCtx, cancel := context.WithCancel(ctx)
		addr, err := dialSSH(tunnelCtx, *db.SSH, db.Host, db.Port)
		if err != nil {
			cancel()
			return engine.Source{}, nil, err
		}
		release = cancel
		var errSplit error
		host, port, errSplit = splitAddr(addr)
		if errSplit != nil {
			cancel()
			return engine.Source{}, nil, errSplit
		}
	}
	return engine.Source{
		Engine:          db.Engine,
		Host:            host,
		Port:            port,
		User:            db.Username,
		Password:        db.Password,
		Database:        db.Database,
		SSL:             db.SSL,
		Version:         db.Version,
		IncludeTriggers: db.IncludeTriggers,
		IncludeEvents:   db.IncludeEvents,
		Physical:        physical,
	}, release, nil
}

func (d Database) Public() Database {
	out := d
	out.HasPassword = d.Password != "" || d.HasPassword
	out.Password = ""
	if out.SSH != nil {
		ssh := *out.SSH
		ssh.Password = ""
		ssh.Key = ""
		out.SSH = &ssh
	}
	return out
}

func checkEngine(name string) error {
	switch name {
	case engine.MySQL, engine.MariaDB, engine.PostgreSQL, engine.MongoDB:
		return nil
	default:
		return fmt.Errorf("engine must be mysql, mariadb, postgresql or mongodb")
	}
}

func (a *App) sealDatabase(d *Database) error {
	var err error
	d.Password, err = a.box.Seal(d.Password)
	if err != nil {
		return err
	}
	if d.SSH != nil {
		d.SSH.Password, err = a.box.Seal(d.SSH.Password)
		if err != nil {
			return err
		}
		d.SSH.Key, err = a.box.Seal(d.SSH.Key)
	}
	return err
}

func (a *App) openDatabase(d *Database) error {
	var err error
	d.Password, err = a.box.Open(d.Password)
	if err != nil {
		return err
	}
	d.HasPassword = d.Password != ""
	if d.SSH != nil {
		d.SSH.Password, err = a.box.Open(d.SSH.Password)
		if err != nil {
			return err
		}
		d.SSH.Key, err = a.box.Open(d.SSH.Key)
	}
	return err
}

var storageSecrets = []string{"password", "secretKey", "accountKey", "accessToken", "key", "secret"}

func (a *App) sealSettings(raw json.RawMessage, fields []string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return []byte("{}"), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for _, name := range fields {
		value, ok := obj[name].(string)
		if !ok || value == "" {
			continue
		}
		sealed, err := a.box.Seal(value)
		if err != nil {
			return nil, err
		}
		obj[name] = sealed
	}
	return json.Marshal(obj)
}

func (a *App) openSettings(raw json.RawMessage, fields []string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return []byte("{}"), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for _, name := range fields {
		value, ok := obj[name].(string)
		if !ok || value == "" {
			continue
		}
		opened, err := a.box.Open(value)
		if err != nil {
			return nil, err
		}
		obj[name] = opened
	}
	return json.Marshal(obj)
}

func maskSettings(raw json.RawMessage, fields []string) json.RawMessage {
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	for _, name := range fields {
		if value, ok := obj[name].(string); ok && value != "" {
			obj[name] = ""
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func (a *App) track(id string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.cancels[id] = cancel
	a.mu.Unlock()
}

func (a *App) untrack(id string) {
	a.mu.Lock()
	delete(a.cancels, id)
	a.mu.Unlock()
}

func (a *App) Cancel(id string) {
	a.mu.Lock()
	cancel := a.cancels[id]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
