package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The metadata repo holds the connection password and the storage secret in
// sealed form, and the records it keeps must not hold the plaintext. The
// audit trail is a record too, so a secret that leaked into a log entry
// would be caught here as well.
func TestSecretsAreNotStoredInPlaintext(t *testing.T) {
	application := openApp(t)
	ctx := context.Background()

	const password = "dmsx-plaintext-password-probe"
	const secret = "dmsx-plaintext-secret-probe"

	database, err := application.CreateDatabase(ctx, Database{
		Name: "shop", Engine: "mysql", Host: "127.0.0.1", Port: 3306,
		Username: "root", Password: password, Database: "shop",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{
		"bucket": "b", "region": "us-east-1", "endpoint": "https://s3.example",
		"accessKey": "ak", "secretKey": secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := application.CreateStorage(ctx, Storage{Name: "bucket", Type: "S3", Settings: raw})
	if err != nil {
		t.Fatal(err)
	}
	application.Audit(ctx, "database.update", "shop (mysql 127.0.0.1:3306/shop)")

	kinds := []string{
		kindDatabase, kindStorage, kindConfig, kindRun, kindArchive,
		kindDDL, kindRestore, kindNotifier, kindAudit,
	}
	for _, kind := range kinds {
		var bodies []json.RawMessage
		if err := application.repo.List(ctx, kind, &bodies); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind == kindDatabase || kind == kindStorage || kind == kindAudit {
			if len(bodies) == 0 {
				t.Fatalf("the %s kind holds no record to scan", kind)
			}
		}
		for _, body := range bodies {
			if strings.Contains(string(body), password) {
				t.Fatalf("the %s record holds the password in plaintext: %s", kind, body)
			}
			if strings.Contains(string(body), secret) {
				t.Fatalf("the %s record holds the storage secret in plaintext: %s", kind, body)
			}
		}
	}

	// The sealed value is what the repo holds, and it is not the plaintext.
	var stored Database
	if err := application.repo.Get(ctx, kindDatabase, database.ID, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Password == "" || stored.Password == password {
		t.Fatalf("the stored password %q", stored.Password)
	}
	var storedStorage Storage
	if err := application.repo.Get(ctx, kindStorage, storage.ID, &storedStorage); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedStorage.Settings), secret) {
		t.Fatalf("the stored settings hold the secret: %s", storedStorage.Settings)
	}
}
