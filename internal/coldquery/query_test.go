package coldquery

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dmsx/internal/archive"
)

func TestS3GlobUsesPrefixAndSkipTLS(t *testing.T) {
	glob, err := sourceGlob(Request{Database: "shop", Table: "orders", Bucket: "bucket", Prefix: "tenant/a"})
	if err != nil {
		t.Fatal(err)
	}
	if glob != "s3://bucket/tenant/a/dms_archives/shop/orders/*.parquet" {
		t.Fatal(glob)
	}
	statements := s3Statements(Request{Endpoint: "https://minio.local", SkipTLS: true, UseSSL: true})
	joined := strings.Join(statements, "\n")
	if !strings.Contains(joined, "SET enable_server_cert_verification=false") || !strings.Contains(joined, "SET s3_use_ssl=true") {
		t.Fatal(joined)
	}
}

func TestLocalParquetQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2024_archive.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	sent := false
	_, err = archive.WriteParquet(file, []archive.Column{{Name: "id", Kind: archive.KindInt}, {Name: "note", Kind: archive.KindString}}, func() ([]any, error) {
		if sent {
			return nil, io.EOF
		}
		sent = true
		return []any{int64(7), "kept"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := Query(context.Background(), Request{
		Database:  "shop",
		Table:     "orders",
		Where:     "id = 7",
		Limit:     100,
		LocalGlob: filepath.Join(dir, "*.parquet"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["note"] != "kept" {
		t.Fatalf("%v", rows)
	}
	if _, err := Query(context.Background(), Request{Database: "shop", Table: "orders", Where: "id = 1; DROP TABLE x", LocalGlob: path}); err == nil {
		t.Fatal("unsafe filter was accepted")
	}
}

// The filter of a cold query must not be able to read a file outside the
// archive, and it must not be able to read the settings of the session,
// which carry the storage secret.
func TestQueryRejectsFiltersThatReachOutsideTheArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2024_archive.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	filters := []string{
		"note = (SELECT note FROM parquet_scan('/etc/passwd'))",
		"note = (select note from parquet_scan('/etc/passwd'))",
		"parquet_scan('/etc/passwd')",
		"duckdb_settings()",
		"current_setting('s3_secret_access_key')",
		"note = current_setting('s3_secret_access_key')",
		"read_parquet('/etc/passwd')",
		"note = abs(-1)",
		"note = (SELECT 1)",
	}
	for _, filter := range filters {
		if _, err := Query(context.Background(), Request{Database: "shop", Table: "orders", Where: filter, LocalGlob: path}); err == nil {
			t.Fatalf("%q was accepted", filter)
		}
	}
}
