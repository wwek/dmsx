package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// Memory is an in-process destination used by tests.
type Memory struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{objs: map[string][]byte{}}
}

func (m *Memory) Put(_ context.Context, key string, r io.Reader) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = body
	return nil
}

func (m *Memory) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.objs[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), body...))), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *Memory) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objs[key]
	return ok, nil
}

func (m *Memory) Test(ctx context.Context) error { return testObject(ctx, m) }
