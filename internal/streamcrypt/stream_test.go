package streamcrypt

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/google/uuid"
)

func TestRoundTrip(t *testing.T) {
	plain := make([]byte, (1<<20)+50)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	var stored bytes.Buffer
	id := uuid.New()
	w, err := NewWriter(&stored, "master-secret", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte(nil), stored.Bytes()...)
	r, err := NewReader(bytes.NewReader(raw), "master-secret")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("len got %d want %d", len(got), len(plain))
	}
	wrong, err := NewReader(bytes.NewReader(raw), "other-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(wrong); err == nil {
		t.Fatal("wrong master key was accepted")
	}
}
