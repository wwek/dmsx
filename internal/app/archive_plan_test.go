package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dmsx/internal/schedule"
)

func TestUpdateArchivePlanToggle(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()

	db, err := application.CreateDatabase(ctx, Database{
		Name: "shop", Engine: "mysql", Host: "127.0.0.1", Port: 3306,
		Username: "root", Password: "x", Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	pathJSON, _ := json.Marshal(filepath.Join(t.TempDir(), "arc"))
	st, err := application.CreateStorage(ctx, Storage{
		Name: "local", Type: "LOCAL",
		Settings: []byte(`{"path":` + string(pathJSON) + `}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tod := "03:00"
	plan, err := application.CreateArchivePlan(ctx, ArchivePlan{
		Name: "orders", DatabaseID: db.ID, StorageID: st.ID,
		Table: "orders", Where: "id < 10", Year: 2024, BatchSize: 1000,
		Schedule: schedule.Schedule{Type: schedule.TypeDaily, TimeOfDay: &tod},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.Enabled = false
	updated, err := application.UpdateArchivePlan(ctx, plan.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("expected disabled")
	}
	if updated.LastRunAt != nil {
		t.Fatal("lastRunAt should stay empty")
	}
}

func TestDueArchivePlanStartsJob(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	db, st := archiveFixtures(t, application)
	tod := "03:00"
	plan, err := application.CreateArchivePlan(ctx, ArchivePlan{
		Name: "orders", DatabaseID: db.ID, StorageID: st.ID,
		Table: "orders", Where: "id < 10", Year: 2024,
		Schedule: schedule.Schedule{Type: schedule.TypeHourly, TimeOfDay: &tod},
	})
	if err != nil {
		t.Fatal(err)
	}
	application.scheduleArchivePass(ctx, time.Now().UTC())
	jobs, err := application.ListArchives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].PlanID != plan.ID || jobs[0].Status == "" {
		t.Fatalf("%+v", jobs)
	}
}

func TestArchiveRejectsGoogleDrive(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	db, _ := archiveFixtures(t, application)
	drive, err := application.CreateStorage(ctx, Storage{
		Name: "drive", Type: "GOOGLE_DRIVE", Settings: []byte(`{"accessToken":"token"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.CreateArchivePlan(ctx, ArchivePlan{
		Name: "orders", DatabaseID: db.ID, StorageID: drive.ID,
		Table: "orders", Where: "id < 10", Year: 2024,
		Schedule: schedule.Schedule{Type: schedule.TypeHourly},
	})
	if err == nil || !strings.Contains(err.Error(), "Google Drive") {
		t.Fatal(err)
	}
}

func TestColdQueryRejectsPostgreSQL(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	_, st := archiveFixtures(t, application)
	pg, err := application.CreateDatabase(ctx, Database{
		Name: "pg", Engine: "postgresql", Host: "127.0.0.1", Port: 5432,
		Username: "postgres", Password: "x", Database: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.ColdQuery(ctx, ColdRequest{DatabaseID: pg.ID, StorageID: st.ID, Table: "orders", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "MySQL") {
		t.Fatal(err)
	}
}

func TestArchiveRejectsPostgreSQL(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	_, st := archiveFixtures(t, application)
	pg, err := application.CreateDatabase(ctx, Database{
		Name: "pg", Engine: "postgresql", Host: "127.0.0.1", Port: 5432,
		Username: "postgres", Password: "x", Database: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.StartArchive(ctx, ArchiveJob{
		DatabaseID: pg.ID, StorageID: st.ID, Table: "orders", Where: "id < 10", Year: 2024,
	})
	if err == nil || !strings.Contains(err.Error(), "MySQL") {
		t.Fatal(err)
	}
}

func archiveFixtures(t *testing.T, application *App) (Database, Storage) {
	t.Helper()
	ctx := context.Background()
	db, err := application.CreateDatabase(ctx, Database{
		Name: "shop", Engine: "mysql", Host: "127.0.0.1", Port: 3306,
		Username: "root", Password: "x", Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	pathJSON, _ := json.Marshal(filepath.Join(t.TempDir(), "arc"))
	st, err := application.CreateStorage(ctx, Storage{
		Name: "local", Type: "LOCAL",
		Settings: []byte(`{"path":` + string(pathJSON) + `}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, st
}
