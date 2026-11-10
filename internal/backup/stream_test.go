package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/engine"
	"dmsx/internal/storage"
)

// The backup body must not touch the disk on its way to the storage. The
// pipeline runs with the temporary directory pointed at an empty folder and
// the folder has to stay empty.
func TestBackupWritesNoStagingFile(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)

	mem := storage.NewMemory()
	chunk := bytes.Repeat([]byte("dmsx"), 32<<10)
	_, err := run(context.Background(), func(_ context.Context, w io.Writer) (engine.Format, error) {
		for i := 0; i < 16; i++ {
			if _, err := w.Write(chunk); err != nil {
				return engine.Format{}, err
			}
		}
		return engine.Format{Name: "mysql-sql", Extension: ".sql.zst", CompressInPipeline: true}, nil
	}, true, true, "master", uuid.New(), mem, "backups/one.sql.zst.enc", engine.Format{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("the backup left files behind: %v", names)
	}
}

// A slow upload has to stall the dump. That is what makes the pipe the
// transport: a dump that runs ahead of the storage would mean the bytes went
// through a buffer or a file.
func TestSlowStorageStallsTheDump(t *testing.T) {
	store := &slowStore{Store: storage.NewMemory(), gate: make(chan struct{}), reading: make(chan struct{})}
	dumped := make(chan struct{})
	go func() {
		defer close(dumped)
		chunk := bytes.Repeat([]byte("dmsx"), 64<<10)
		_, _ = run(context.Background(), func(_ context.Context, w io.Writer) (engine.Format, error) {
			for i := 0; i < 8; i++ {
				if _, err := w.Write(chunk); err != nil {
					return engine.Format{}, err
				}
			}
			return engine.Format{Name: "mysql-sql", Extension: ".sql.zst", CompressInPipeline: true}, nil
		}, false, false, "", uuid.New(), store, "backups/slow.sql.zst", engine.Format{}, 0, nil)
	}()

	select {
	case <-store.reading:
	case <-time.After(5 * time.Second):
		t.Fatal("the storage never started reading")
	}
	select {
	case <-dumped:
		t.Fatal("the dump ran ahead of the storage")
	case <-time.After(300 * time.Millisecond):
	}
	close(store.gate)
	select {
	case <-dumped:
	case <-time.After(5 * time.Second):
		t.Fatal("the dump did not finish after the storage caught up")
	}
}

// slowStore reads the body, then waits on a gate before reading the rest.
type slowStore struct {
	storage.Store
	gate    chan struct{}
	reading chan struct{}
	once    sync.Once
}

func (s *slowStore) Put(ctx context.Context, key string, body io.Reader) error {
	buffer := make([]byte, 1024)
	if _, err := body.Read(buffer); err != nil {
		return err
	}
	s.once.Do(func() { close(s.reading) })
	select {
	case <-s.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Store.Put(ctx, key, body)
}
