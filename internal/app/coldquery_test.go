package app

import (
	"context"
	"strings"
	"testing"
)

// The table of a cold query names a directory under the storage root. A name
// that walks out of it must not reach the file system.
func TestColdQueryRejectsTableNamesThatLeaveTheStorageRoot(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	db, st := archiveFixtures(t, application)

	names := []string{
		"../../../../../../../../etc",
		"..",
		"orders/../../..",
		"/etc",
		"orders; DROP TABLE orders",
	}
	for _, name := range names {
		_, err := application.ColdQuery(ctx, ColdRequest{
			DatabaseID: db.ID, StorageID: st.ID, Table: name, Where: "id = 1", Limit: 10,
		})
		if err == nil {
			t.Fatalf("table %q was accepted", name)
		}
		if !strings.Contains(err.Error(), "plain identifier") {
			t.Fatalf("table %q: %v", name, err)
		}
	}
}

// A plain table name passes the check and reaches the archive.
func TestColdQueryAcceptsAPlainTableName(t *testing.T) {
	application, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	ctx := context.Background()
	db, st := archiveFixtures(t, application)

	_, err = application.ColdQuery(ctx, ColdRequest{
		DatabaseID: db.ID, StorageID: st.ID, Table: "orders", Where: "id = 1", Limit: 10,
	})
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "plain identifier") {
		t.Fatalf("the table name was rejected: %v", err)
	}
}
