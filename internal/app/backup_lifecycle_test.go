package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteDatabaseRemovesItsBackups(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	directory := t.TempDir()
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, directory)
	run := BackupRun{
		ID:         "run-1",
		DatabaseID: database.ID,
		StorageID:  storage.ID,
		Status:     "success",
		FileName:   "backups/run-1.sql.zst",
		Codec:      "mysql-sql",
		StartedAt:  time.Now().UTC(),
	}
	if err := os.MkdirAll(filepath.Join(directory, "backups"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, run.FileName), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := application.cat.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	config := BackupConfig{ID: "cfg-1", Name: "nightly", DatabaseID: database.ID, StorageID: storage.ID, Method: "logical", CreatedAt: time.Now().UTC()}
	if err := application.cat.Put(ctx, kindConfig, config.ID, config); err != nil {
		t.Fatal(err)
	}

	if err := application.DeleteDatabase(ctx, database.ID); err != nil {
		t.Fatal(err)
	}
	runs, err := application.ListRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs left: %+v", runs)
	}
	if _, err := os.Stat(filepath.Join(directory, run.FileName)); !os.IsNotExist(err) {
		t.Fatalf("backup file still present: %v", err)
	}
}

func TestPhysicalBackupIsNotPipedBack(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	run := BackupRun{ID: "phys", Status: "success", Codec: "pg-base-server-zstd:5", StartedAt: time.Now().UTC()}
	if err := application.cat.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	_, err := application.StartRestore(ctx, run.ID, RestoreTarget{Host: "127.0.0.1", Port: 5432, Username: "postgres", Database: "shop"})
	if err == nil || err.Error() == "" {
		t.Fatal("expected physical restore to be refused")
	}
}

func openApp(t *testing.T) *App {
	t.Helper()
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	return application
}

func mustDatabase(t *testing.T, application *App) Database {
	t.Helper()
	database, err := application.CreateDatabase(context.Background(), Database{
		Name: "shop", Engine: "mysql", Host: "127.0.0.1", Port: 3306, Username: "root", Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func mustLocalStorage(t *testing.T, application *App, directory string) Storage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"path": directory})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := application.CreateStorage(context.Background(), Storage{Name: "disk", Type: "LOCAL", Settings: raw})
	if err != nil {
		t.Fatal(err)
	}
	return storage
}
