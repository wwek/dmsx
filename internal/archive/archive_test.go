package archive

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"io"
	"sync"
	"testing"
)

func TestPrefixedKeyMatchesColdQuery(t *testing.T) {
	if got := PrefixedKey("tenant/a", "dms_archives/shop/orders/2024_archive.parquet"); got != "tenant/a/dms_archives/shop/orders/2024_archive.parquet" {
		t.Fatal(got)
	}
	if got := PrefixedKey("", "dms_archives/shop/orders/2024_archive.parquet"); got != "dms_archives/shop/orders/2024_archive.parquet" {
		t.Fatal(got)
	}
}

func TestObjectKey(t *testing.T) {
	key, err := ObjectKey("shop", "orders", 2024, "")
	if err != nil {
		t.Fatal(err)
	}
	if key != "dms_archives/shop/orders/2024_archive.parquet" {
		t.Fatal(key)
	}
	if err := (Request{Database: "shop", Table: "orders", Year: 2024, Where: "create_time < '2025-01-01'"}).validate(); err != nil {
		t.Fatal(err)
	}
}

type memTable struct {
	pk      PrimaryKey
	cols    []Column
	rows    [][]any
	deleted [][]any
}

func (m *memTable) PrimaryKey(context.Context) (PrimaryKey, error) { return m.pk, nil }
func (m *memTable) Columns(context.Context) ([]Column, error)      { return m.cols, nil }
func (m *memTable) Close()                                         {}

func (m *memTable) FetchChunk(_ context.Context, after []any, limit int) ([][]any, error) {
	var out [][]any
	for _, row := range m.rows {
		if len(after) > 0 && !keyGreater(row[:len(m.pk)], after) {
			continue
		}
		if deleted(m.deleted, row[:len(m.pk)]) {
			continue
		}
		out = append(out, append([]any(nil), row...))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *memTable) DeleteKeys(_ context.Context, keys [][]any) (int64, error) {
	var n int64
	for _, key := range keys {
		if deleted(m.deleted, key) {
			continue
		}
		m.deleted = append(m.deleted, append([]any(nil), key...))
		n++
	}
	return n, nil
}

func keyGreater(a, b []any) bool {
	for i := range a {
		ai, _ := a[i].(int64)
		bi, _ := b[i].(int64)
		if ai > bi {
			return true
		}
		if ai < bi {
			return false
		}
	}
	return false
}

func deleted(set [][]any, key []any) bool {
	for _, existing := range set {
		same := len(existing) == len(key)
		if same {
			for i := range key {
				if existing[i] != key[i] {
					same = false
					break
				}
			}
		}
		if same {
			return true
		}
	}
	return false
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

func TestArchiveChunksByPrimaryKey(t *testing.T) {
	table := &memTable{
		pk:   PrimaryKey{"id"},
		cols: []Column{{Name: "id", Kind: KindInt}, {Name: "note", Kind: KindString}},
		rows: [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}, {int64(4), "d"}, {int64(5), "e"}},
	}
	dest := &memDest{}
	result, err := Run(context.Background(), Request{
		Database: "shop", Table: "orders", Year: 2024, Where: "id > 0", DeleteAfter: true, BatchSize: 2,
	}, table, dest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsArchived != 5 || result.RowsDeleted != 5 || len(result.Keys) != 3 {
		t.Fatalf("%+v", result)
	}
	sum := md5.Sum(dest.objs[result.Key])
	if result.MD5 != fmtHex(sum[:]) {
		t.Fatalf("md5")
	}
}

func TestArchiveDoesNotDeleteWhenMD5Differs(t *testing.T) {
	table := &memTable{pk: PrimaryKey{"id"}, cols: []Column{{Name: "id", Kind: KindInt}}, rows: [][]any{{int64(1)}}}
	bad := &badDest{}
	_, err := Run(context.Background(), Request{Database: "shop", Table: "orders", Year: 2024, Where: "id < 10", DeleteAfter: true}, table, bad, nil, nil)
	if err == nil || len(table.deleted) != 0 {
		t.Fatalf("err=%v deleted=%v", err, table.deleted)
	}
}

func TestArchiveCancelStopsBetweenBatches(t *testing.T) {
	table := &memTable{pk: PrimaryKey{"id"}, cols: []Column{{Name: "id", Kind: KindInt}}, rows: [][]any{{int64(1)}, {int64(2)}, {int64(3)}, {int64(4)}}}
	ctx, cancel := context.WithCancel(context.Background())
	batch := 0
	_, err := Run(ctx, Request{Database: "shop", Table: "orders", Year: 2024, Where: "id > 0", DeleteAfter: true, BatchSize: 2}, table, &memDest{}, nil, func(p Progress) {
		if p.Phase == PhaseDone && p.RowsArchived > 0 {
			batch++
			if batch == 1 {
				cancel()
			}
		}
	})
	if !errors.Is(err, context.Canceled) || len(table.deleted) != 2 {
		t.Fatalf("err=%v deleted=%v", err, table.deleted)
	}
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

func fmtHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0x0f]
	}
	return string(out)
}
