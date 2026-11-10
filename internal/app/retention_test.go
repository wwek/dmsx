package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dmsx/internal/retention"
)

// A count keeps the failed and the cancelled runs. A time window drops every
// run older than the window, whatever its status was.
func TestRetentionStatusHandling(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	directory := t.TempDir()
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, directory)
	cfg := BackupConfig{ID: "cfg", DatabaseID: database.ID, StorageID: storage.ID}

	old := time.Now().Add(-48 * time.Hour)
	runs := []struct {
		id     string
		status string
	}{
		{"11111111-1111-1111-1111-111111111111", "success"},
		{"22222222-2222-2222-2222-222222222222", "failed"},
		{"33333333-3333-3333-3333-333333333333", "cancelled"},
	}
	for _, item := range runs {
		run := BackupRun{ID: item.id, ConfigID: cfg.ID, DatabaseID: database.ID, StorageID: storage.ID,
			Status: item.status, FileName: "backups/" + item.id, Bytes: 1, StartedAt: old}
		if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
			t.Fatal(err)
		}
	}
	older := "55555555-5555-5555-5555-555555555555"
	if err := application.repo.Put(ctx, kindRun, older, BackupRun{ID: older, ConfigID: cfg.ID, DatabaseID: database.ID,
		StorageID: storage.ID, Status: "success", FileName: "backups/" + older, Bytes: 1,
		StartedAt: old.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	cfg.Retention = retention.Policy{Kind: retention.KindCount, Count: 1}
	application.applyRetention(ctx, cfg)
	left := runStatuses(t, application)
	if len(left) != 3 {
		t.Fatalf("count policy kept %v", left)
	}
	if left["22222222-2222-2222-2222-222222222222"] != "failed" ||
		left["33333333-3333-3333-3333-333333333333"] != "cancelled" {
		t.Fatalf("count policy dropped a failed or cancelled run: %v", left)
	}
	if _, ok := left[older]; ok {
		t.Fatalf("count policy kept the older successful run: %v", left)
	}

	cfg.Retention = retention.Policy{Kind: retention.KindTime, Period: retention.PeriodDay}
	application.applyRetention(ctx, cfg)
	left = runStatuses(t, application)
	if len(left) != 0 {
		t.Fatalf("time window kept %v", left)
	}
}

func runStatuses(t *testing.T, application *App) map[string]string {
	t.Helper()
	runs, err := application.ListRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, run := range runs {
		out[run.ID] = run.Status
	}
	return out
}

func TestRetentionDeletesTheStoredObjectOfAnExpiredRun(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	directory := t.TempDir()
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, directory)
	cfg := BackupConfig{ID: "cfg", DatabaseID: database.ID, StorageID: storage.ID,
		Retention: retention.Policy{Kind: retention.KindTime, Period: retention.PeriodDay}}
	id := "44444444-4444-4444-4444-444444444444"
	run := BackupRun{ID: id, ConfigID: cfg.ID, DatabaseID: database.ID, StorageID: storage.ID,
		Status: "failed", FileName: "backups/" + id, Bytes: 1, StartedAt: time.Now().Add(-48 * time.Hour)}
	if err := os.MkdirAll(filepath.Join(directory, "backups"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, run.FileName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
		t.Fatal(err)
	}
	application.applyRetention(ctx, cfg)
	if _, err := os.Stat(filepath.Join(directory, run.FileName)); !os.IsNotExist(err) {
		t.Fatalf("the object of an expired run survived: %v", err)
	}
}
