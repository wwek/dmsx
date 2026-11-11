package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A database that goes away takes its archive jobs and its WAL segments with
// it, and the objects of those jobs leave the storage too.
func TestDeleteDatabaseRemovesArchiveJobsAndWalSegments(t *testing.T) {
	ctx := context.Background()
	application := openApp(t)
	database := mustDatabase(t, application)
	storage := mustLocalStorage(t, application, t.TempDir())
	store, _, err := application.store(ctx, storage.ID)
	if err != nil {
		t.Fatal(err)
	}

	archiveKey := "dms_archives/shop/orders/2024_archive_1.parquet"
	walKey := "wal/shop/000000010000000000000001"
	for _, key := range []string{archiveKey, walKey} {
		if err := store.Put(ctx, key, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	job := ArchiveJob{ID: "job-1", DatabaseID: database.ID, StorageID: storage.ID, Table: "orders",
		Status: "success", ObjectKey: archiveKey, ObjectKeys: []string{archiveKey}, StartedAt: time.Now().UTC()}
	if err := application.repo.Put(ctx, kindArchive, job.ID, job); err != nil {
		t.Fatal(err)
	}
	segment := WalSegment{ID: "seg-1", DatabaseID: database.ID, StorageID: storage.ID,
		Name: "000000010000000000000001", ObjectKey: walKey, Bytes: 1, UploadedAt: time.Now().UTC()}
	if err := application.repo.Put(ctx, kindWalSegment, segment.ID, segment); err != nil {
		t.Fatal(err)
	}

	if err := application.DeleteDatabase(ctx, database.ID); err != nil {
		t.Fatal(err)
	}

	var jobs []ArchiveJob
	if err := application.repo.List(ctx, kindArchive, &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("the archive job survived: %+v", jobs)
	}
	var segments []WalSegment
	if err := application.repo.List(ctx, kindWalSegment, &segments); err != nil {
		t.Fatal(err)
	}
	if len(segments) != 0 {
		t.Fatalf("the WAL segment survived: %+v", segments)
	}
	for _, key := range []string{archiveKey, walKey} {
		exists, err := store.Exists(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("the object %q survived the database", key)
		}
	}
}
