//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"dmsx/internal/archive"
	"dmsx/internal/coldquery"
)

// TestProbeColdQueryLoop runs the S3 cold query many times against the
// endpoint to see whether the failure comes back in a short loop.
func TestProbeColdQueryLoop(t *testing.T) {
	if os.Getenv("DMSX_PROBE_LOOP") == "" {
		t.Skip("the probe loop is not requested")
	}
	bucket := newS3Server(t)
	ctx := context.Background()

	columns := []archive.Column{{Name: "id", Kind: archive.KindInt}, {Name: "note", Kind: archive.KindString}}
	rows := [][]any{{int64(1), "old-a"}}
	key := "dmsx/dms_archives/shop/orders/2024_archive_probe_1.parquet"
	bucket.put("archives/"+key, parquetBytes(t, columns, rows))

	req := coldquery.Request{
		Database: "shop", Table: "orders", Where: "id = 1", Limit: 100,
		Bucket: "archives", Region: "us-east-1", Endpoint: bucket.endpoint(),
		AccessKey: "dmsx", SecretKey: "dmsx-secret", PathStyle: true, UseSSL: false,
		Prefix: "dmsx",
	}
	for i := 1; i <= 30; i++ {
		start := time.Now()
		out, err := coldquery.Query(ctx, req)
		if err != nil {
			t.Fatalf("round %d failed after %s: %v\nrequests %v", i, time.Since(start), err, bucket.trace())
		}
		if len(out) != 1 || fmt.Sprint(out[0]["note"]) != "old-a" {
			t.Fatalf("round %d returned %v", i, out)
		}
	}
	t.Log("30 rounds done")
}

func parquetBytes(t *testing.T, columns []archive.Column, rows [][]any) []byte {
	t.Helper()
	path := t.TempDir() + "/probe.parquet"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	i := 0
	if _, err := archive.WriteParquet(file, columns, func() ([]any, error) {
		if i >= len(rows) {
			return nil, io.EOF
		}
		row := rows[i]
		i++
		return row, nil
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
