package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

// ManifestFile is one regular file hashed while the base-backup tar streams by.
type ManifestFile struct {
	Path        string
	Size        int64
	ModTime     time.Time
	ChecksumHex string
}

var rawWalSegmentRe = regexp.MustCompile(`^pg_wal/[0-9A-F]{24}$`)

// WalkBaseBackupTar hashes every regular file that belongs in backup_manifest.
// Raw WAL segment members are skipped, matching PostgreSQL's own list.
func WalkBaseBackupTar(r io.Reader, codec string) ([]ManifestFile, error) {
	body, closer, err := openBaseBackupReader(r, codec)
	if err != nil {
		return nil, err
	}
	defer closer()

	tr := tar.NewReader(body)
	hasher := sha256.New()
	var files []ManifestFile
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}
		if header.Typeflag != tar.TypeReg || header.Name == "backup_manifest" || rawWalSegmentRe.MatchString(header.Name) {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		hasher.Reset()
		if _, err := io.Copy(hasher, tr); err != nil {
			return nil, fmt.Errorf("hash %q: %w", header.Name, err)
		}
		files = append(files, ManifestFile{
			Path:        header.Name,
			Size:        header.Size,
			ModTime:     header.ModTime,
			ChecksumHex: hex.EncodeToString(hasher.Sum(nil)),
		})
	}
	return files, nil
}

func openBaseBackupReader(r io.Reader, codec string) (io.Reader, func(), error) {
	switch {
	case strings.Contains(codec, "zstd"):
		dec, err := zstd.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return dec, dec.Close, nil
	case strings.Contains(codec, "gzip"):
		dec, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, err
		}
		return dec, func() { _ = dec.Close() }, nil
	default:
		return r, func() {}, nil
	}
}

// BuildBackupManifest writes a PostgreSQL backup_manifest v2 body that
// pg_basebackup --incremental can consume.
func BuildBackupManifest(files []ManifestFile, systemID uint64, timelineID int, startLSN, stopLSN string) ([]byte, error) {
	if systemID == 0 {
		return nil, fmt.Errorf("system identifier is required for a backup manifest")
	}
	if timelineID == 0 {
		timelineID = 1
	}
	var body bytes.Buffer
	write := func(s string) { _, _ = body.WriteString(s) }
	write(`{ "PostgreSQL-Backup-Manifest-Version": 2,` + "\n")
	write(`"System-Identifier": ` + strconv.FormatUint(systemID, 10) + ",\n")
	write(`"Files": [`)
	for i, file := range files {
		if i == 0 {
			write("\n")
		} else {
			write(",\n")
		}
		path, err := escapePGJSON(file.Path)
		if err != nil {
			return nil, err
		}
		write(`{ "Path": ` + path + ", ")
		write(`"Size": ` + strconv.FormatInt(file.Size, 10) + ", ")
		write(`"Last-Modified": "` + file.ModTime.UTC().Format("2006-01-02 15:04:05") + ` GMT"`)
		write(`, "Checksum-Algorithm": "SHA256", "Checksum": "` + file.ChecksumHex + `"`)
		write(" }")
	}
	write("\n],\n")
	write(`"WAL-Ranges": [` + "\n")
	write(fmt.Sprintf(`{ "Timeline": %d, "Start-LSN": "%s", "End-LSN": "%s" }`, timelineID, startLSN, stopLSN))
	write("\n],\n")
	sum := sha256.Sum256(body.Bytes())
	write(`"Manifest-Checksum": "` + hex.EncodeToString(sum[:]) + `"}` + "\n")
	return body.Bytes(), nil
}

func escapePGJSON(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("manifest path is not UTF-8")
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
