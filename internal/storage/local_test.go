package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalRoundTripStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	raw, err := json.Marshal(localSettings{Path: root})
	if err != nil {
		t.Fatal(err)
	}
	store, err := newLocal(raw)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "dmsx-connection-test", strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "dmsx-connection-test"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body %q", body)
	}
	if err := store.Put(ctx, "../outside", strings.NewReader("no")); err == nil {
		t.Fatal("key escaped the storage directory")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside")); err == nil {
		t.Fatal("escaped file was written")
	}
}
