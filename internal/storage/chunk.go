package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	// DefaultPartSize is the multipart part held in memory. One part is uploaded
	// and acknowledged before the next is read, so a slow bucket only slows the
	// producer. It does not grow the buffer with the database.
	DefaultPartSize = 16 * 1024 * 1024
	// DefaultMaxParts stays under the 1000-part cap used by several S3-compatible
	// providers. A larger dump rolls into the next object and is described by a
	// manifest written last.
	DefaultMaxParts = 1000
	manifestSuffix  = ".parts"
	partSuffix      = ".part"
	partDigits      = 6
	manifestVersion = 1
)

type Layout struct {
	PartSize int
	MaxParts int
}

func (l Layout) normalize() Layout {
	if l.PartSize <= 0 {
		l.PartSize = DefaultPartSize
	}
	if l.MaxParts <= 0 {
		l.MaxParts = DefaultMaxParts
	}
	return l
}

// Part is one completed multipart part.
type Part struct {
	Number int
	ETag   string
}

// Blob is the small surface the chunk writer needs from S3.
type Blob interface {
	Put(ctx context.Context, key string, data []byte, sendMD5 bool) error
	Start(ctx context.Context, key string) (uploadID string, err error)
	PutPart(ctx context.Context, key, uploadID string, partNumber int, data []byte) (etag string, err error)
	Complete(ctx context.Context, key, uploadID string, parts []Part) error
	Abort(ctx context.Context, key, uploadID string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Remove(ctx context.Context, key string) error
}

type manifest struct {
	Version   int    `json:"version"`
	TotalSize int64  `json:"totalSize"`
	Parts     []span `json:"parts"`
}

type span struct {
	Key    string `json:"key"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// WriteChunked stores r as one object when it fits in a single part, and as a
// manifest plus part objects otherwise. The manifest is written last, so a
// reader that finds it knows every part was committed.
func WriteChunked(ctx context.Context, blob Blob, baseKey string, r io.Reader, layout Layout) error {
	layout = layout.normalize()
	first := make([]byte, layout.PartSize)
	n, err := io.ReadFull(r, first)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fmt.Errorf("read backup stream: %w", err)
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return blob.Put(ctx, baseKey, first[:n], true)
	}
	peek := make([]byte, 1)
	pn, peekErr := io.ReadFull(r, peek)
	if peekErr != nil && peekErr != io.EOF {
		return fmt.Errorf("read backup stream: %w", peekErr)
	}
	if pn == 0 {
		return blob.Put(ctx, baseKey, first, true)
	}
	source := io.MultiReader(bytes.NewReader(first), bytes.NewReader(peek[:pn]), r)
	return writeParts(ctx, blob, baseKey, source, layout)
}

func writeParts(ctx context.Context, blob Blob, baseKey string, source io.Reader, layout Layout) error {
	buf := make([]byte, layout.PartSize)
	var uploaded []string
	var spans []span
	var total int64

	abandon := func(objectKey, uploadID string) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if uploadID != "" {
			_ = blob.Abort(cleanup, objectKey, uploadID)
		}
		for _, key := range uploaded {
			_ = blob.Remove(cleanup, key)
		}
	}

	objectIndex := 1
	ended := false
	for !ended {
		objectKey := partKey(baseKey, objectIndex)
		uploadID, err := blob.Start(ctx, objectKey)
		if err != nil {
			abandon("", "")
			return fmt.Errorf("start multipart upload: %w", err)
		}
		var parts []Part
		sum := sha256.New()
		var size int64
		for number := 1; number <= layout.MaxParts; number++ {
			if err := ctx.Err(); err != nil {
				abandon(objectKey, uploadID)
				return err
			}
			n, readErr := io.ReadFull(source, buf)
			if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
				abandon(objectKey, uploadID)
				return fmt.Errorf("read backup stream: %w", readErr)
			}
			if n == 0 && readErr == io.EOF {
				ended = true
				break
			}
			etag, err := blob.PutPart(ctx, objectKey, uploadID, number, buf[:n])
			if err != nil {
				abandon(objectKey, uploadID)
				return err
			}
			sum.Write(buf[:n])
			parts = append(parts, Part{Number: number, ETag: etag})
			size += int64(n)
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				ended = true
				break
			}
		}
		if len(parts) == 0 {
			_ = blob.Abort(ctx, objectKey, uploadID)
			break
		}
		if err := blob.Complete(ctx, objectKey, uploadID, parts); err != nil {
			abandon(objectKey, uploadID)
			return fmt.Errorf("complete multipart upload: %w", err)
		}
		uploaded = append(uploaded, objectKey)
		spans = append(spans, span{Key: objectKey, Size: size, SHA256: hex.EncodeToString(sum.Sum(nil))})
		total += size
		objectIndex++
	}

	body, err := json.Marshal(manifest{Version: manifestVersion, TotalSize: total, Parts: spans})
	if err != nil {
		abandon("", "")
		return err
	}
	if err := blob.Put(ctx, baseKey+manifestSuffix, body, true); err != nil {
		abandon("", "")
		return fmt.Errorf("write chunk manifest: %w", err)
	}
	return nil
}

func partKey(base string, index int) string {
	return fmt.Sprintf("%s%s%0*d", base, partSuffix, partDigits, index)
}

func isPartKey(base, candidate string) bool {
	prefix := base + partSuffix
	if len(candidate) != len(prefix)+partDigits || candidate[:len(prefix)] != prefix {
		return false
	}
	for _, r := range candidate[len(prefix):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// OpenChunked returns the stored bytes. A manifest is verified part by part
// before those bytes are handed to the caller. A missing manifest is not
// treated as a shorter successful backup.
func OpenChunked(ctx context.Context, blob Blob, baseKey string) (io.ReadCloser, error) {
	raw, err := blob.Get(ctx, baseKey+manifestSuffix)
	if err == nil {
		var doc manifest
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("chunk manifest: %w", err)
		}
		if doc.Version != manifestVersion || len(doc.Parts) == 0 {
			return nil, fmt.Errorf("chunk manifest for %s is not usable", baseKey)
		}
		return &partReader{ctx: ctx, blob: blob, parts: doc.Parts, total: doc.TotalSize}, nil
	}
	body, getErr := blob.Get(ctx, baseKey)
	if getErr != nil {
		return nil, fmt.Errorf("open %s: manifest missing and object unavailable: %w", baseKey, getErr)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

type partReader struct {
	ctx    context.Context
	blob   Blob
	parts  []span
	index  int
	buf    []byte
	off    int
	read   int64
	total  int64
	closed bool
}

func (r *partReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.EOF
	}
	if r.off >= len(r.buf) {
		if r.index >= len(r.parts) {
			if r.read != r.total {
				return 0, fmt.Errorf("chunked object ended at %d bytes, manifest says %d", r.read, r.total)
			}
			return 0, io.EOF
		}
		span := r.parts[r.index]
		r.index++
		body, err := r.blob.Get(r.ctx, span.Key)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", span.Key, err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != span.SHA256 || int64(len(body)) != span.Size {
			return 0, fmt.Errorf("%s failed the manifest checksum", span.Key)
		}
		r.buf = body
		r.off = 0
	}
	n := copy(p, r.buf[r.off:])
	r.off += n
	r.read += int64(n)
	return n, nil
}

func (r *partReader) Close() error {
	r.closed = true
	return nil
}

// RemoveChunked deletes the object, its manifest, and every derived part.
func RemoveChunked(ctx context.Context, blob Blob, baseKey string, extra []string) error {
	var first error
	remember := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	raw, err := blob.Get(ctx, baseKey+manifestSuffix)
	if err == nil {
		var doc manifest
		if json.Unmarshal(raw, &doc) == nil {
			for _, span := range doc.Parts {
				remember(blob.Remove(ctx, span.Key))
			}
		}
	}
	remember(blob.Remove(ctx, baseKey+manifestSuffix))
	remember(blob.Remove(ctx, baseKey))
	for _, key := range extra {
		if key == baseKey || key == baseKey+manifestSuffix || isPartKey(baseKey, key) {
			remember(blob.Remove(ctx, key))
		}
	}
	return first
}
