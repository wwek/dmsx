package archive

import (
	"context"
	"crypto/md5"
	"fmt"
	"hash"
	"io"

	"dmsx/internal/sqlident"
)

// Result is what an archive run proved before it was allowed to delete rows.
type Result struct {
	Key          string
	MD5          string
	RowsArchived int64
	RowsDeleted  int64
}

// Source is a forward-only cursor. Next returns io.EOF when the cursor ends.
type Source interface {
	Columns(ctx context.Context) ([]Column, error)
	Next(ctx context.Context) ([]any, error)
}

// Destination accepts one object and can read it back. The read-back is how
// the MD5 is confirmed: the bytes are hashed and discarded, not written to a
// local file.
type Destination interface {
	Upload(ctx context.Context, key string, body io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Deleter removes archived rows in small batches. It is called only after the
// remote MD5 matches.
type Deleter interface {
	DeleteBatches(ctx context.Context, batch int, pause func()) (int64, error)
}

type Request struct {
	Database    string
	Table       string
	Where       string
	Year        int
	BatchSize   int
	DeleteAfter bool
}

func (r Request) validate() error {
	if _, err := ObjectKey(r.Database, r.Table, r.Year, ""); err != nil {
		return err
	}
	return sqlident.ValidatePredicate(r.Where)
}

// Run streams rows into a parquet writer, uploads that stream, checks the
// remote MD5, and only then deletes. A failed check leaves the primary rows
// in place.
func Run(ctx context.Context, req Request, source Source, dest Destination, deleter Deleter, pause func()) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 5000
	}
	key, err := ObjectKey(req.Database, req.Table, req.Year, "")
	if err != nil {
		return Result{}, err
	}
	exists, err := dest.Exists(ctx, key)
	if err != nil {
		return Result{}, err
	}
	if exists {
		key, err = ObjectKey(req.Database, req.Table, req.Year, Stamp())
		if err != nil {
			return Result{}, err
		}
	}
	columns, err := source.Columns(ctx)
	if err != nil {
		return Result{}, err
	}

	reader, writer := io.Pipe()
	sum := md5.New()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- dest.Upload(ctx, key, reader)
	}()

	var rows int64
	writeErr := make(chan error, 1)
	go func() {
		defer close(writeErr)
		n, err := WriteParquet(io.MultiWriter(writer, sum), columns, func() ([]any, error) {
			return source.Next(ctx)
		})
		rows = n
		if err != nil {
			_ = writer.CloseWithError(err)
			writeErr <- err
			return
		}
		writeErr <- writer.Close()
	}()

	putErr := <-uploadErr
	bodyErr := <-writeErr
	if bodyErr != nil {
		return Result{}, bodyErr
	}
	if putErr != nil {
		return Result{}, fmt.Errorf("upload %s: %w", key, putErr)
	}
	digest := append([]byte(nil), sum.Sum(nil)...)
	if err := verifyMD5(ctx, dest, key, digest); err != nil {
		return Result{Key: key, MD5: fmt.Sprintf("%x", digest), RowsArchived: rows}, err
	}
	result := Result{Key: key, MD5: fmt.Sprintf("%x", digest), RowsArchived: rows}
	if !req.DeleteAfter {
		return result, nil
	}
	if deleter == nil {
		return result, fmt.Errorf("delete was requested but no deleter was configured")
	}
	deleted, err := deleter.DeleteBatches(ctx, req.BatchSize, pause)
	result.RowsDeleted = deleted
	if err != nil {
		return result, fmt.Errorf("remote md5 matched but delete failed after %d rows: %w", deleted, err)
	}
	return result, nil
}

func verifyMD5(ctx context.Context, dest Destination, key string, want []byte) error {
	body, err := dest.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("open uploaded archive: %w", err)
	}
	defer body.Close()
	got := md5.New()
	if _, err := io.Copy(got, body); err != nil {
		return fmt.Errorf("hash uploaded archive: %w", err)
	}
	if !equalHash(got, want) {
		return fmt.Errorf("remote md5 for %s does not match the stream; rows were not deleted", key)
	}
	return nil
}

func equalHash(sum hash.Hash, want []byte) bool {
	return string(sum.Sum(nil)) == string(want)
}
