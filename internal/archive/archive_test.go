package archive

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestObjectKey(t *testing.T) {
	key, err := ObjectKey("shop", "orders", 2024, "")
	if err != nil {
		t.Fatal(err)
	}
	if key != "dms_archives/shop/orders/2024_archive.parquet" {
		t.Fatal(key)
	}
	if _, err := ObjectKey("shop", "orders;drop", 2024, ""); err == nil {
		t.Fatal("identifier was accepted")
	}
	if err := sqlPredicate(); err != nil {
		t.Fatal(err)
	}
}

func sqlPredicate() error {
	return validateWhere("create_time < '2025-01-01'")
}

func validateWhere(text string) error {
	req := Request{Database: "shop", Table: "orders", Year: 2024, Where: text}
	return req.validate()
}

type sliceSource struct {
	cols []Column
	rows [][]any
	i    int
}

func (s *sliceSource) Columns(context.Context) ([]Column, error) { return s.cols, nil }
func (s *sliceSource) Next(context.Context) ([]any, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	row := s.rows[s.i]
	s.i++
	return row, nil
}

type memDest struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *memDest) Upload(_ context.Context, key string, body io.Reader) error {
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.objs == nil {
		m.objs = map[string][]byte{}
	}
	m.objs[key] = buf
	return nil
}

func (m *memDest) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objs[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (m *memDest) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objs[key]
	return ok, nil
}

type countingDelete struct{ n int64 }

func (d *countingDelete) DeleteBatches(context.Context, int, func()) (int64, error) {
	d.n = 2
	return 2, nil
}

func TestArchiveVerifiesMD5BeforeDelete(t *testing.T) {
	src := &sliceSource{
		cols: []Column{{Name: "id", Kind: KindInt}, {Name: "create_time", Kind: KindString}},
		rows: [][]any{{int64(1), "2024-02-01"}, {int64(2), "2024-03-01"}},
	}
	dest := &memDest{}
	del := &countingDelete{}
	result, err := Run(context.Background(), Request{
		Database: "shop", Table: "orders", Year: 2024,
		Where: "create_time < '2025-01-01'", DeleteAfter: true,
	}, src, dest, del, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsArchived != 2 || result.RowsDeleted != 2 {
		t.Fatalf("%+v", result)
	}
	body := dest.objs[result.Key]
	sum := md5.Sum(body)
	if result.MD5 != fmtHex(sum[:]) {
		t.Fatalf("md5 %s vs %x", result.MD5, sum)
	}
	if err := validateWhere("create_time < '2025-01-01'; DROP TABLE orders"); err == nil {
		t.Fatal("statement break was accepted")
	}
	_ = time.Second
}

func fmtHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0x0f]
	}
	return string(out)
}

type badDest struct{ memDest }

func (b *badDest) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := b.memDest.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if len(body) > 0 {
		body[0] ^= 0xff
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func TestArchiveDoesNotDeleteWhenMD5Differs(t *testing.T) {
	src := &sliceSource{
		cols: []Column{{Name: "id", Kind: KindInt}},
		rows: [][]any{{int64(1)}},
	}
	del := &countingDelete{}
	_, err := Run(context.Background(), Request{
		Database: "shop", Table: "orders", Year: 2024,
		Where: "id < 10", DeleteAfter: true,
	}, src, &badDest{}, del, func() {})
	if err == nil {
		t.Fatal("expected md5 failure")
	}
	if del.n != 0 {
		t.Fatal("rows were deleted after a bad checksum")
	}
}
