package engine

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestWalkAndBuildManifest(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("17\n")
	header := &tar.Header{Name: "PG_VERSION", Mode: 0o600, Size: int64(len(content)), ModTime: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	wal := make([]byte, 16)
	walHeader := &tar.Header{Name: "pg_wal/000000010000000000000001", Mode: 0o600, Size: int64(len(wal)), ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(walHeader); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(wal); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := WalkBaseBackupTar(bytes.NewReader(buf.Bytes()), "none")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "PG_VERSION" {
		t.Fatalf("%+v", files)
	}
	sum := sha256.Sum256(content)
	if files[0].ChecksumHex != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum %s", files[0].ChecksumHex)
	}

	manifest, err := BuildBackupManifest(files, 42, 1, "0/100", "0/200")
	if err != nil {
		t.Fatal(err)
	}
	text := string(manifest)
	for _, want := range []string{
		`"PostgreSQL-Backup-Manifest-Version": 2`,
		`"System-Identifier": 42`,
		`"Path": "PG_VERSION"`,
		`"Start-LSN": "0/100"`,
		`"Manifest-Checksum":`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	if strings.Contains(text, "pg_wal/000000010000000000000001") {
		t.Fatal("raw WAL segment must stay out of the manifest")
	}
}
