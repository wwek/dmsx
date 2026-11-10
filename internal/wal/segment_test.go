package wal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListCompleteSegments(t *testing.T) {
	dir := t.TempDir()
	name := "000000010000000000000001"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, DefaultSegmentSize), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".partial"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir, DefaultSegmentSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Complete || got[0].TimelineID != 1 {
		t.Fatalf("%+v", got)
	}
}
