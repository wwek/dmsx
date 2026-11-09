// Package backup connects a dump tool to storage without a local staging file.
//
// Bytes travel dump stdout, then zstd for plain SQL, then optional AES-GCM,
// then the storage writer. The storage reads a pipe, so a slow upload applies
// backpressure to the dump instead of filling a disk cache.
package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"dmsx/internal/engine"
	"dmsx/internal/storage"
	"dmsx/internal/streamcrypt"
)

type Result struct {
	Format engine.Format
	Bytes  int64
	SHA256 string
}

type dumpFunc func(context.Context, io.Writer) (engine.Format, error)

// ErrTooLarge is returned when the stored stream passes the plan's per-backup
// limit. The partial object is deleted.
var ErrTooLarge = errors.New("backup is larger than the size limit")

// Run streams one dump to dest. maxBytes caps the stored size; 0 is no limit.
func Run(ctx context.Context, src engine.Source, encrypt bool, master string, fileID uuid.UUID, dest storage.Store, key string, maxBytes int64, onBytes func(int64)) (Result, error) {
	return run(ctx, func(ctx context.Context, w io.Writer) (engine.Format, error) {
		return engine.Dump(ctx, src, w)
	}, src.Engine == engine.MySQL || src.Engine == engine.MariaDB, encrypt, master, fileID, dest, key, declaredFormat(src), maxBytes, onBytes)
}

func run(ctx context.Context, dump dumpFunc, compress, encrypt bool, master string, fileID uuid.UUID, dest storage.Store, key string, fallback engine.Format, maxBytes int64, onBytes func(int64)) (Result, error) {
	reader, writer := io.Pipe()
	putErr := make(chan error, 1)
	go func() {
		err := dest.Put(ctx, key, reader)
		_ = reader.Close()
		putErr <- err
	}()

	sum := sha256.New()
	counter := &countWriter{w: io.MultiWriter(writer, sum), max: maxBytes, onBytes: onBytes, next: time.Now().Add(5 * time.Second)}
	var closers []interface{ Close() error }
	out := io.Writer(counter)

	fail := func(err error) (Result, error) {
		_ = writer.CloseWithError(err)
		<-putErr
		_ = dest.Delete(context.WithoutCancel(ctx), key)
		return Result{}, err
	}

	if encrypt {
		enc, err := streamcrypt.NewWriter(out, master, fileID)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, enc)
		out = enc
	}
	if compress {
		encoder, err := zstd.NewWriter(out, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(5)))
		if err != nil {
			return fail(err)
		}
		closers = append(closers, encoder)
		out = encoder
	}

	format, dumpErr := dump(ctx, out)
	var closeErr error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if dumpErr != nil {
		_ = writer.CloseWithError(dumpErr)
	} else if closeErr != nil {
		_ = writer.CloseWithError(closeErr)
	} else if err := writer.Close(); err != nil && closeErr == nil {
		closeErr = err
	}
	putFailure := <-putErr
	if counter.exceeded {
		_ = dest.Delete(context.WithoutCancel(ctx), key)
		return Result{}, fmt.Errorf("%w (%d MB)", ErrTooLarge, maxBytes>>20)
	}
	if dumpErr != nil {
		_ = dest.Delete(context.WithoutCancel(ctx), key)
		return Result{}, dumpErr
	}
	if closeErr != nil {
		_ = dest.Delete(context.WithoutCancel(ctx), key)
		return Result{}, closeErr
	}
	if putFailure != nil {
		_ = dest.Delete(context.WithoutCancel(ctx), key)
		return Result{}, fmt.Errorf("save to storage: %w", putFailure)
	}
	if format.Name == "" {
		format = fallback
	}
	return Result{Format: format, Bytes: counter.n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

func declaredFormat(src engine.Source) engine.Format {
	switch src.Engine {
	case engine.MySQL:
		return engine.Format{Name: "mysql-sql", Extension: ".sql.zst", CompressInPipeline: true}
	case engine.MariaDB:
		return engine.Format{Name: "mariadb-sql", Extension: ".sql.zst", CompressInPipeline: true}
	case engine.MongoDB:
		return engine.Format{Name: "mongo-archive", Extension: ".archive.gz"}
	default:
		if src.Physical {
			return engine.Format{Name: "pg-base", Extension: ".tar"}
		}
		return engine.Format{Name: "pg-custom", Extension: ".dump"}
	}
}

type countWriter struct {
	w        io.Writer
	n        int64
	max      int64
	exceeded bool
	onBytes  func(int64)
	next     time.Time
}

func (c *countWriter) Write(p []byte) (int, error) {
	if c.max > 0 && c.n+int64(len(p)) > c.max {
		c.exceeded = true
		return 0, ErrTooLarge
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.onBytes != nil && time.Now().After(c.next) {
		c.next = time.Now().Add(5 * time.Second)
		c.onBytes(c.n)
	}
	return n, err
}

// OpenStored unwraps encryption and pipeline zstd. Tool-compressed archives
// (pg_dump custom, mongodump gzip, pg_basebackup tar) are returned as stored.
func OpenStored(master string, format engine.Format, encrypted bool, body io.Reader) (io.Reader, error) {
	var err error
	if encrypted {
		body, err = streamcrypt.NewReader(body, master)
		if err != nil {
			return nil, err
		}
	}
	if format.CompressInPipeline {
		decoder, err := zstd.NewReader(body)
		if err != nil {
			return nil, err
		}
		return decoder, nil
	}
	return body, nil
}
