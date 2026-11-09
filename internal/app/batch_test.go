package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dmsx/internal/healthcheck"
	"dmsx/internal/notify"
	"dmsx/internal/retention"
)

func TestNotifierSecretsAreSealedAndMasked(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	raw := json.RawMessage(`{"botToken":"123:secret","chatId":"42"}`)
	created, err := application.CreateNotifier(ctx, Notifier{Name: "ops", Type: "telegram", Settings: raw, Events: []string{"BACKUP_FAILED"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(created.Settings), "secret") {
		t.Fatalf("response settings %s", created.Settings)
	}
	var stored Notifier
	if err := application.cat.Get(ctx, kindNotifier, created.ID, &stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored.Settings), "123:secret") {
		t.Fatal("bot token stored in clear")
	}
	listed, err := application.ListNotifiers(ctx)
	if err != nil || len(listed) != 1 || strings.Contains(string(listed[0].Settings), "secret") {
		t.Fatalf("list %+v %v", listed, err)
	}
	if _, err := application.CreateNotifier(ctx, Notifier{Name: "x", Type: "telegram", Settings: raw, Events: []string{"NOPE"}}); err == nil {
		t.Fatal("unknown event accepted")
	}
}

func TestBackupFailureReachesSubscribedWebhook(t *testing.T) {
	received := make(chan map[string]string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		received <- body
	}))
	defer server.Close()

	application := openApp(t)
	ctx := context.Background()
	database := mustDatabase(t, application)
	other := mustDatabase(t, application)
	settings, _ := json.Marshal(map[string]string{"url": server.URL})
	if _, err := application.CreateNotifier(ctx, Notifier{Name: "hook", Type: "WEBHOOK", Settings: settings, Events: []string{"BACKUP_FAILED"}, DatabaseIDs: []string{database.ID}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cfg := BackupConfig{ID: "cfg", Name: "nightly", DatabaseID: database.ID}
	application.notifyBackup(ctx, cfg, BackupRun{Status: "success", StartedAt: now, FinishedAt: &now})
	application.notifyBackup(ctx, BackupConfig{Name: "other", DatabaseID: other.ID}, BackupRun{Status: "failed", Error: "x", StartedAt: now, FinishedAt: &now})
	application.notifyBackup(ctx, cfg, BackupRun{Status: "failed", Error: "access denied", StartedAt: now, FinishedAt: &now})

	select {
	case body := <-received:
		if body["event"] != string(notify.EventBackupFailed) || !strings.Contains(body["message"], "access denied") || !strings.Contains(body["heading"], database.Name) {
			t.Fatalf("body %+v", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}
	select {
	case body := <-received:
		t.Fatalf("unsubscribed event or database delivered: %+v", body)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestTotalSizeCapDeletesOldestBackups(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	directory := t.TempDir()
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, directory)
	cfg := BackupConfig{ID: "cfg", DatabaseID: database.ID, StorageID: storage.ID,
		Retention: retention.Policy{Kind: retention.KindCount, Count: 10}, MaxTotalSizeMB: 2}
	if err := os.MkdirAll(filepath.Join(directory, "backups"), 0o750); err != nil {
		t.Fatal(err)
	}
	ids := []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"}
	for i, id := range ids {
		run := BackupRun{ID: id, ConfigID: cfg.ID, DatabaseID: database.ID, StorageID: storage.ID, Status: "success",
			FileName: "backups/" + id, Bytes: 1 << 20, StartedAt: time.Now().Add(time.Duration(i) * time.Hour)}
		if err := os.WriteFile(filepath.Join(directory, run.FileName), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := application.cat.Put(ctx, kindRun, run.ID, run); err != nil {
			t.Fatal(err)
		}
	}
	application.applyRetention(ctx, cfg)
	runs, _ := application.ListRuns(ctx)
	if len(runs) != 2 {
		t.Fatalf("runs left %d", len(runs))
	}
	for _, run := range runs {
		if run.ID == ids[0] {
			t.Fatal("oldest backup survived the cap")
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "backups", ids[0])); !os.IsNotExist(err) {
		t.Fatalf("oldest object still stored: %v", err)
	}
}

func TestAuditRecordsActionsWithoutSecrets(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	if _, err := application.CreateDatabase(ctx, Database{Name: "shop", Engine: "mysql", Host: "h", Port: 3306, Username: "root", Password: "hunter22", Database: "shop"}); err != nil {
		t.Fatal(err)
	}
	if err := application.SetOperatorPassword(ctx, "operator-pw"); err != nil {
		t.Fatal(err)
	}
	entries, err := application.ListAudit(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries %+v", entries)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Detail, "hunter22") || strings.Contains(entry.Detail, "operator-pw") {
			t.Fatalf("secret in audit: %+v", entry)
		}
	}
}

func TestResetPasswordMovesChangedAt(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	if err := application.SetOperatorPassword(ctx, "first-password"); err != nil {
		t.Fatal(err)
	}
	before := application.PasswordChangedAt(ctx)
	time.Sleep(5 * time.Millisecond)
	if err := application.ResetOperatorPassword(ctx, "second-password"); err != nil {
		t.Fatal(err)
	}
	if !application.PasswordChangedAt(ctx).After(before) {
		t.Fatal("changedAt did not move")
	}
	if application.CheckOperatorPassword(ctx, "first-password") == nil {
		t.Fatal("old password still accepted")
	}
	if err := application.CheckOperatorPassword(ctx, "second-password"); err != nil {
		t.Fatal(err)
	}
}

func TestHealthConfigAndState(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()
	database := mustDatabase(t, application)
	if _, err := application.SetHealthCheck(ctx, database.ID, healthcheck.Config{Enabled: true, IntervalMinutes: 0, FailuresBeforeDown: 3}); err == nil {
		t.Fatal("interval 0 accepted")
	}
	updated, err := application.SetHealthCheck(ctx, database.ID, healthcheck.Config{Enabled: false, IntervalMinutes: 5, FailuresBeforeDown: 2})
	if err != nil {
		t.Fatal(err)
	}
	if updated.HealthCheck == nil || updated.HealthCheck.Enabled {
		t.Fatalf("config %+v", updated.HealthCheck)
	}
	states, err := application.ListHealth(ctx)
	if err != nil || len(states) != 1 || states[0].DatabaseID != database.ID {
		t.Fatalf("states %+v %v", states, err)
	}
}
