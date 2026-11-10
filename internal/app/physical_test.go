package app

import (
	"context"
	"testing"
	"time"
)

func TestLatestPhysicalParent(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()

	older := BackupRun{
		ID: "11111111-1111-1111-1111-111111111111", ConfigID: "cfg", Status: "success",
		Codec: "pg-base-server-zstd:5", ManifestKey: "a.manifest",
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	newer := BackupRun{
		ID: "22222222-2222-2222-2222-222222222222", ConfigID: "cfg", Status: "success",
		Codec: "pg-base-server-zstd:5", ManifestKey: "b.manifest",
		StartedAt: time.Now().UTC(),
	}
	logical := BackupRun{
		ID: "33333333-3333-3333-3333-333333333333", ConfigID: "cfg", Status: "success",
		Codec: "pg-custom", StartedAt: time.Now().UTC().Add(time.Minute),
	}
	for _, run := range []BackupRun{older, newer, logical} {
		if err := application.repo.Put(ctx, kindRun, run.ID, run); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := application.latestPhysicalParent(ctx, "cfg")
	if !ok || got.ID != newer.ID {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}
