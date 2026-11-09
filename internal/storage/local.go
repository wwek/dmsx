package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type localSettings struct {
	Path string `json:"path"`
}

type localStore struct{ root string }

func newLocal(raw json.RawMessage) (Store, error) {
	var settings localSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(settings.Path)
	if err != nil {
		return nil, err
	}
	return &localStore{root: abs}, nil
}

func (s *localStore) path(key string) (string, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	clean := filepath.Clean(key)
	if key == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("object key escapes the storage directory")
	}
	full := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("object key escapes the storage directory")
	}
	return full, nil
}

func (s *localStore) Put(_ context.Context, key string, r io.Reader) error {
	full, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (s *localStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (s *localStore) Delete(_ context.Context, key string) error {
	full, err := s.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(full)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *localStore) Exists(_ context.Context, key string) (bool, error) {
	full, err := s.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(full)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *localStore) Test(ctx context.Context) error {
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return err
	}
	return testObject(ctx, s)
}

func (s *localStore) Root() string { return s.root }

func AsLocal(store Store) (string, bool) {
	local, ok := store.(*localStore)
	if !ok {
		return "", false
	}
	return local.Root(), true
}
