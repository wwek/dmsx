package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dmsx/internal/retention"
	"dmsx/internal/schedule"
)

func TestUpdateDatabaseKeepsPasswordWhenBlank(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	created, err := application.CreateDatabase(ctx, Database{
		Name: "shop", Engine: "mysql", Host: "127.0.0.1", Port: 3306,
		Username: "root", Password: "secret-pw", Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.UpdateDatabase(ctx, created.ID, Database{
		Name: "shop-2", Engine: "mysql", Host: "127.0.0.1", Port: 3306,
		Username: "root", Password: "", Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "shop-2" || updated.Password != "" {
		t.Fatalf("%+v", updated)
	}
	opened, err := application.database(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Password != "secret-pw" {
		t.Fatalf("password %q", opened.Password)
	}
}

func TestUpdateStorageKeepsSecretKey(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	settings, _ := json.Marshal(map[string]string{
		"endpoint": "https://s3.example", "bucket": "b", "accessKey": "ak", "secretKey": "sk-secret", "region": "us-east-1",
	})
	created, err := application.CreateStorage(ctx, Storage{Name: "s3", Type: "S3", Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(created.Settings), "sk-secret") {
		t.Fatal("secret leaked")
	}
	next, _ := json.Marshal(map[string]string{
		"endpoint": "https://s3.example", "bucket": "b2", "accessKey": "ak", "secretKey": "", "region": "us-east-1",
	})
	updated, err := application.UpdateStorage(ctx, created.ID, Storage{Name: "s3b", Type: "S3", Settings: next})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "s3b" {
		t.Fatalf("%+v", updated)
	}
	_, row, err := application.store(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var opened map[string]string
	_ = json.Unmarshal(row.Settings, &opened)
	if opened["secretKey"] != "sk-secret" || opened["bucket"] != "b2" {
		t.Fatalf("%+v", opened)
	}
}

func TestUpdateConfigCanDisablePlan(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, t.TempDir())
	tod := "03:00"
	created, err := application.CreateConfig(ctx, BackupConfig{
		Name: "nightly", DatabaseID: database.ID, StorageID: storage.ID, Method: "logical",
		Schedule:  schedule.Schedule{Type: schedule.TypeDaily, TimeOfDay: &tod},
		Retention: retention.Policy{Kind: retention.KindCount, Count: 7},
		Retry:     true, MaxFailedTries: 5,
		IncludeSchemas: []string{"public"}, ExcludeTables: []string{"logs"},
		MaxBackupSizeMB: 100, MaxTotalSizeMB: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Enabled || created.MaxFailedTries != 5 {
		t.Fatalf("%+v", created)
	}
	created.Enabled = false
	created.Name = "paused"
	created.MaxFailedTries = 2
	updated, err := application.UpdateConfig(ctx, created.ID, created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.Name != "paused" || updated.MaxFailedTries != 2 {
		t.Fatalf("%+v", updated)
	}
	listed, err := application.ListConfigs(ctx)
	if err != nil || len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("%+v %v", listed, err)
	}
}
