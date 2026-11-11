package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"dmsx/internal/retention"
)

// mustUnreachableStorage returns a storage that cannot be reached, so every
// delete of an object in it fails.
func mustUnreachableStorage(t *testing.T, application *App) Storage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"bucket": "b", "region": "us-east-1", "endpoint": "http://127.0.0.1:1",
		"accessKey": "ak", "secretKey": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := application.CreateStorage(context.Background(), Storage{
		Name: "unreachable", Type: "S3", Settings: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	return storage
}

// A storage that cannot be reached fails every delete. The run of an object
// that stays keeps its row, so the deletion can be tried again later, and the
// error is kept on the run.
func TestRetentionKeepsTheRunWhenTheObjectStays(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	database := mustDatabase(t, application)
	storage := mustUnreachableStorage(t, application)
	cfg := BackupConfig{ID: "cfg", DatabaseID: database.ID, StorageID: storage.ID,
		Retention: retention.Policy{Kind: retention.KindTime, Period: retention.PeriodDay}}
	id := "66666666-6666-6666-6666-666666666666"
	run := BackupRun{ID: id, ConfigID: cfg.ID, DatabaseID: database.ID, StorageID: storage.ID,
		Status: "success", FileName: "backups/" + id, Bytes: 1, StartedAt: time.Now().Add(-48 * time.Hour)}
	if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	application.applyRetention(ctx, cfg)
	var stored BackupRun
	if err := application.repo.Get(ctx, kindRun, id, &stored); err != nil {
		t.Fatalf("the run of the object that stayed is gone: %v", err)
	}
	if !strings.Contains(stored.Error, "not deleted") {
		t.Fatalf("the error was not kept on the run: %q", stored.Error)
	}
}

// The same holds when the operator deletes a run by hand: the row stays while
// the object stays.
func TestDeleteRunKeepsTheRunWhenTheObjectStays(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	database := mustDatabase(t, application)
	storage := mustUnreachableStorage(t, application)
	id := "77777777-7777-7777-7777-777777777777"
	run := BackupRun{ID: id, DatabaseID: database.ID, StorageID: storage.ID,
		Status: "success", FileName: "backups/" + id, Bytes: 1, StartedAt: time.Now().UTC()}
	if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	if err := application.DeleteRun(ctx, id); err == nil {
		t.Fatal("the run was deleted although the object stayed")
	}
	var stored BackupRun
	if err := application.repo.Get(ctx, kindRun, id, &stored); err != nil {
		t.Fatalf("the run of the object that stayed is gone: %v", err)
	}
}

// A run whose object was deleted before keeps the old behaviour: the row goes
// away with it.
func TestDeleteRunRemovesTheRunWhenTheObjectIsGone(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, t.TempDir())
	id := "88888888-8888-8888-8888-888888888888"
	run := BackupRun{ID: id, DatabaseID: database.ID, StorageID: storage.ID,
		Status: "success", FileName: "backups/" + id, Bytes: 1, StartedAt: time.Now().UTC()}
	if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	if err := application.DeleteRun(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := application.repo.Get(ctx, kindRun, id, &BackupRun{}); err == nil {
		t.Fatal("the run is still there")
	}
}
