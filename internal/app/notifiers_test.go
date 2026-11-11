package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dmsx/internal/notify"
)

// The url of a webhook the operator runs often carries a token, so it is a
// secret: it stays sealed in the metadata repo and it is blanked in the API.
func TestWebhookURLIsSealedAndMasked(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()

	const endpoint = "https://hooks.example/dmsx"
	const token = "dmsx-webhook-token-probe"
	settings, err := json.Marshal(map[string]string{"url": endpoint + "?token=" + token})
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.CreateNotifier(ctx, Notifier{
		Name: "hook", Type: notify.TypeWebhook, Settings: settings,
		Events: []string{string(notify.EventBackupFailed)},
	})
	if err != nil {
		t.Fatal(err)
	}

	var stored Notifier
	if err := application.repo.Get(ctx, kindNotifier, created.ID, &stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored.Settings), token) {
		t.Fatalf("the record holds the webhook token in plaintext: %s", stored.Settings)
	}

	rows, err := application.ListNotifiers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d notifiers", len(rows))
	}
	if strings.Contains(string(rows[0].Settings), token) {
		t.Fatalf("the API returns the webhook token: %s", rows[0].Settings)
	}
	var public map[string]string
	if err := json.Unmarshal(rows[0].Settings, &public); err != nil {
		t.Fatal(err)
	}
	if public["url"] != "" {
		t.Fatalf("the API returns the webhook url: %q", public["url"])
	}

	opened, err := application.openSettings(stored.Settings, notify.SecretFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), token) {
		t.Fatalf("the sealed url does not open: %s", opened)
	}
}
