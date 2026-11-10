// Package storage is the destination side of a diskless backup.
// Dump bytes are read from an io.Reader and written to the destination. The
// backup is not staged in a local file first.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	TypeLocal       = "LOCAL"
	TypeS3          = "S3"
	TypeGCS         = "GCS"
	TypeAzure       = "AZURE_BLOB"
	TypeOSS         = "OSS"
	TypeSwift       = "SWIFT"
	TypeSSH         = "SSH"
	TypeQiniu       = "QINIU"
	TypeSFTP        = "SFTP"
	TypeFTP         = "FTP"
	TypeGoogleDrive = "GOOGLE_DRIVE"
	TypeNAS         = "NAS"
	TypeRclone      = "RCLONE"
)

// Store reads and writes one object key.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Test(ctx context.Context) error
}

func New(kind string, settings json.RawMessage) (Store, error) {
	switch strings.ToUpper(kind) {
	case TypeLocal:
		return newLocal(settings)
	case TypeS3:
		return newS3(settings, false)
	case TypeGCS:
		return newS3(defaultGCS(settings), false)
	case TypeOSS:
		return newOSS(settings)
	case TypeQiniu:
		return newS3(settings, true)
	case TypeSwift:
		return newSwift(settings)
	case TypeSSH, TypeSFTP:
		return newSFTP(settings)
	case TypeFTP:
		return newFTP(settings)
	case TypeAzure:
		return newAzure(settings)
	case TypeGoogleDrive:
		return newDrive(settings)
	case TypeNAS:
		return newNAS(settings)
	case TypeRclone:
		return newRclone(settings)
	default:
		return nil, fmt.Errorf("unknown storage type %q", kind)
	}
}

func joinKey(prefix, key string) string {
	prefix = strings.Trim(prefix, "/")
	key = strings.TrimLeft(key, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func testObject(ctx context.Context, store Store) error {
	const key = "dmsx-connection-test"
	if err := store.Put(ctx, key, strings.NewReader("dmsx connection test")); err != nil {
		return err
	}
	return store.Delete(ctx, key)
}
