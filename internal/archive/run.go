package archive

import (
	"context"
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"strconv"

	"dmsx/internal/sqlident"
)

// Result is what an archive run proved. Keys lists every uploaded object.
type Result struct {
	Key          string
	Keys         []string
	MD5          string
	RowsArchived int64
	RowsDeleted  int64
}

const (
	PhaseReading   = "reading"
	PhaseUploading = "uploading"
	PhaseVerifying = "verifying"
	PhaseDeleting  = "deleting"
	PhaseDone      = "done"
)

type Progress struct {
	Phase        string
	RowsArchived int64
	RowsDeleted  int64
	ObjectKey    string
	MD5          string
	Batch        int
}

type Destination interface {
	Upload(ctx context.Context, key string, body io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Table archives one MySQL/MariaDB table in primary-key batches.
type Table interface {
	PrimaryKey(ctx context.Context) (PrimaryKey, error)
	Columns(ctx context.Context) ([]Column, error)
	FetchChunk(ctx context.Context, after []any, limit int) ([][]any, error)
	DeleteKeys(ctx context.Context, keys [][]any) (int64, error)
	Close()
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

// Run archives rows in primary-key batches. Each batch is written to Parquet,
// uploaded, checked with a remote MD5, and only then deleted by those keys.
func Run(ctx context.Context, req Request, table Table, dest Destination, pause func(), onProgress func(Progress)) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 5000
	}
	pk, err := table.PrimaryKey(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := pk.validate(); err != nil {
		return Result{}, err
	}
	columns, err := table.Columns(ctx)
	if err != nil {
		return Result{}, err
	}
	colIndex := map[string]int{}
	for i, column := range columns {
		colIndex[column.Name] = i
	}
	for _, name := range pk {
		if _, ok := colIndex[name]; !ok {
			return Result{}, fmt.Errorf("primary key column %q is not in the table", name)
		}
	}

	var result Result
	var after []any
	stamp := Stamp()
	for batch := 1; ; batch++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		report(onProgress, Progress{Phase: PhaseReading, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, Batch: batch})
		rows, err := table.FetchChunk(ctx, after, req.BatchSize)
		if err != nil {
			return result, err
		}
		if len(rows) == 0 {
			report(onProgress, Progress{Phase: PhaseDone, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, Batch: batch})
			return result, nil
		}
		keys := make([][]any, len(rows))
		for i, row := range rows {
			keys[i], err = pk.extract(row, colIndex)
			if err != nil {
				return result, err
			}
		}
		after = keys[len(keys)-1]
		key, err := ObjectKey(req.Database, req.Table, req.Year, stamp+"_"+strconv.Itoa(batch))
		if err != nil {
			return result, err
		}
		report(onProgress, Progress{Phase: PhaseUploading, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, ObjectKey: key, Batch: batch})
		digest, n, err := uploadBatch(ctx, dest, key, columns, rows)
		if err != nil {
			return result, err
		}
		report(onProgress, Progress{Phase: PhaseVerifying, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, ObjectKey: key, MD5: fmt.Sprintf("%x", digest), Batch: batch})
		if err := verifyMD5(ctx, dest, key, digest); err != nil {
			return result, err
		}
		result.Key = key
		result.Keys = append(result.Keys, key)
		result.MD5 = fmt.Sprintf("%x", digest)
		result.RowsArchived += n
		if req.DeleteAfter {
			report(onProgress, Progress{Phase: PhaseDeleting, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, ObjectKey: key, MD5: result.MD5, Batch: batch})
			deleted, err := table.DeleteKeys(ctx, keys)
			result.RowsDeleted += deleted
			if err != nil {
				return result, fmt.Errorf("remote md5 matched but delete failed after %d rows: %w", deleted, err)
			}
		}
		report(onProgress, Progress{Phase: PhaseDone, RowsArchived: result.RowsArchived, RowsDeleted: result.RowsDeleted, ObjectKey: key, MD5: result.MD5, Batch: batch})
		if pause != nil {
			pause()
		}
	}
}

func report(fn func(Progress), p Progress) {
	if fn != nil {
		fn(p)
	}
}

func uploadBatch(ctx context.Context, dest Destination, key string, columns []Column, rows [][]any) ([]byte, int64, error) {
	reader, writer := io.Pipe()
	sum := md5.New()
	uploadErr := make(chan error, 1)
	go func() {
		uploadErr <- dest.Upload(ctx, key, reader)
	}()
	i := 0
	n, writeErr := WriteParquet(io.MultiWriter(writer, sum), columns, func() ([]any, error) {
		if i >= len(rows) {
			return nil, io.EOF
		}
		row := rows[i]
		i++
		return row, nil
	})
	if writeErr != nil {
		_ = writer.CloseWithError(writeErr)
	} else {
		writeErr = writer.Close()
	}
	putErr := <-uploadErr
	if writeErr != nil {
		return nil, 0, writeErr
	}
	if putErr != nil {
		return nil, 0, fmt.Errorf("upload %s: %w", key, putErr)
	}
	return append([]byte(nil), sum.Sum(nil)...), n, nil
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
