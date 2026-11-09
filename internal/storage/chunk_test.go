package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

type memBlob struct {
	objects map[string][]byte
	open    map[string]map[int][]byte
	aborts  int
}

func newMem() *memBlob {
	return &memBlob{objects: map[string][]byte{}, open: map[string]map[int][]byte{}}
}

func (m *memBlob) Put(_ context.Context, key string, data []byte, _ bool) error {
	m.objects[key] = append([]byte(nil), data...)
	return nil
}

func (m *memBlob) Start(_ context.Context, key string) (string, error) {
	m.open[key] = map[int][]byte{}
	return key, nil
}

func (m *memBlob) PutPart(_ context.Context, key, _ string, partNumber int, data []byte) (string, error) {
	m.open[key][partNumber] = append([]byte(nil), data...)
	return "etag", nil
}

func (m *memBlob) Complete(_ context.Context, key, _ string, parts []Part) error {
	var buf bytes.Buffer
	for _, part := range parts {
		buf.Write(m.open[key][part.Number])
	}
	m.objects[key] = buf.Bytes()
	delete(m.open, key)
	return nil
}

func (m *memBlob) Abort(_ context.Context, key, _ string) error {
	m.aborts++
	delete(m.open, key)
	return nil
}

func (m *memBlob) Get(_ context.Context, key string) ([]byte, error) {
	body, ok := m.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte(nil), body...), nil
}

func (m *memBlob) Remove(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func TestChunkLayouts(t *testing.T) {
	ctx := context.Background()
	layout := Layout{PartSize: 4, MaxParts: 2}

	t.Run("empty", func(t *testing.T) {
		blob := newMem()
		if err := WriteChunked(ctx, blob, "a", bytes.NewReader(nil), layout); err != nil {
			t.Fatal(err)
		}
		if _, ok := blob.objects["a.parts"]; ok {
			t.Fatal("empty object should not have a manifest")
		}
		rc, err := OpenChunked(ctx, blob, "a")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(rc)
		if len(got) != 0 {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("exact part stays one object", func(t *testing.T) {
		blob := newMem()
		body := bytes.Repeat([]byte("z"), 4)
		if err := WriteChunked(ctx, blob, "b", bytes.NewReader(body), layout); err != nil {
			t.Fatal(err)
		}
		if _, ok := blob.objects["b.parts"]; ok {
			t.Fatal("a single part should stay one object")
		}
	})

	t.Run("rolls to the next object", func(t *testing.T) {
		blob := newMem()
		body := make([]byte, 10)
		if _, err := rand.Read(body); err != nil {
			t.Fatal(err)
		}
		if err := WriteChunked(ctx, blob, "c", bytes.NewReader(body), layout); err != nil {
			t.Fatal(err)
		}
		rc, err := OpenChunked(ctx, blob, "c")
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("round trip mismatch")
		}
		if err := RemoveChunked(ctx, blob, "c", nil); err != nil {
			t.Fatal(err)
		}
		if len(blob.objects) != 0 {
			t.Fatalf("objects left: %v", blob.objects)
		}
	})
}

type boom struct {
	left int
}

func (b *boom) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, errors.New("disk full on the source")
	}
	n := min(len(p), b.left)
	b.left -= n
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	return n, nil
}

func TestChunkAbortDropsParts(t *testing.T) {
	blob := newMem()
	err := WriteChunked(context.Background(), blob, "d", &boom{left: 5}, Layout{PartSize: 4, MaxParts: 2})
	if err == nil {
		t.Fatal("expected the source error")
	}
	if blob.aborts == 0 {
		t.Fatal("multipart upload was not aborted")
	}
	if len(blob.objects) != 0 {
		t.Fatalf("partial objects left behind: %v", blob.objects)
	}
}
