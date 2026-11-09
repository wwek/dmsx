package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"dmsx/internal/engine"
	"dmsx/internal/storage"
)

func TestPipelineRoundTripIsDiskless(t *testing.T) {
	mem := storage.NewMemory()
	id := uuid.New()
	result, err := run(context.Background(), func(_ context.Context, w io.Writer) (engine.Format, error) {
		_, err := io.WriteString(w, "CREATE TABLE t(id int);")
		return engine.Format{Name: "mysql-sql", Extension: ".sql.zst", CompressInPipeline: true}, err
	}, true, true, "master", id, mem, "backups/one.sql.zst.enc", engine.Format{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes <= 0 || result.SHA256 == "" {
		t.Fatalf("result %+v", result)
	}
	body, err := mem.Open(context.Background(), "backups/one.sql.zst.enc")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	plain, err := OpenStored("master", result.Format, true, body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CREATE TABLE t(id int);" {
		t.Fatalf("got %q", got)
	}
	// The stored object itself is not plaintext SQL.
	raw, _ := mem.Open(context.Background(), "backups/one.sql.zst.enc")
	defer raw.Close()
	stored, _ := io.ReadAll(raw)
	if bytes.Contains(stored, []byte("CREATE TABLE")) {
		t.Fatal("plaintext leaked into the stored object")
	}
	decoder, err := zstd.NewReader(bytes.NewReader(stored))
	if err == nil {
		defer decoder.Close()
	}
}

func TestPipelineStopsAtSizeLimit(t *testing.T) {
	mem := storage.NewMemory()
	chunk := make([]byte, 64<<10)
	if _, err := rand.Read(chunk); err != nil {
		t.Fatal(err)
	}
	_, err := run(context.Background(), func(_ context.Context, w io.Writer) (engine.Format, error) {
		for i := 0; i < 64; i++ {
			if _, err := w.Write(chunk); err != nil {
				return engine.Format{}, err
			}
		}
		return engine.Format{Name: "pg-custom"}, nil
	}, false, false, "", uuid.New(), mem, "backups/big.dump", engine.Format{}, 1<<20, nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err %v", err)
	}
	if ok, _ := mem.Exists(context.Background(), "backups/big.dump"); ok {
		t.Fatal("partial object left in storage")
	}
}
