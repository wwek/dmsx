//go:build e2e

package e2e

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"testing"

	"dmsx/internal/archive"
	"dmsx/internal/storage"
)

// TestProbeArchiveUploadLoop uploads through the S3 uploader the archive uses
// and reads the object back, the way the MD5 check does, to see whether the
// failure comes back in a short loop.
func TestProbeArchiveUploadLoop(t *testing.T) {
	if os.Getenv("DMSX_PROBE_LOOP") == "" {
		t.Skip("the probe loop is not requested")
	}
	bucket := newS3Server(t)
	ctx := context.Background()
	settings := storage.S3Settings{
		Bucket: "archives", Region: "us-east-1", Endpoint: bucket.endpoint(),
		AccessKey: "dmsx", SecretKey: "dmsx-secret", Prefix: "dmsx",
	}
	uploader, err := archive.NewS3Uploader(settings)
	if err != nil {
		t.Fatal(err)
	}
	columns := []archive.Column{{Name: "id", Kind: archive.KindInt}, {Name: "note", Kind: archive.KindString}}
	rows := [][]any{{int64(1), "old-a"}, {int64(2), "old-b"}}

	for i := 1; i <= 20; i++ {
		key := fmt.Sprintf("dms_archives/shop/orders/2024_archive_probe_%d.parquet", i)
		reader, writer := io.Pipe()
		go func() {
			_, writeErr := archive.WriteParquet(writer, columns, func() ([]any, error) {
				if len(rows) == 0 {
					return nil, io.EOF
				}
				row := rows[0]
				rows = rows[1:]
				return row, nil
			})
			_ = writer.CloseWithError(writeErr)
		}()
		if err := uploader.Upload(ctx, key, reader); err != nil {
			t.Fatalf("round %d upload failed: %v\nrequests %v", i, err, bucket.trace())
		}
		body, err := uploader.Open(ctx, key)
		if err != nil {
			t.Fatalf("round %d reopen failed: %v\nrequests %v", i, err, bucket.trace())
		}
		sum := md5.New()
		if _, err := io.Copy(sum, body); err != nil {
			t.Fatal(err)
		}
		body.Close()
		if hex.EncodeToString(sum.Sum(nil)) == "" {
			t.Fatal("empty digest")
		}
	}
	t.Log("20 upload rounds done")
}
