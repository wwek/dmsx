//go:build ghostembed

package onlineddl

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
)

//go:embed gh-ost
var ghOstBinary []byte

func bundledGhOst() (string, error) {
	sum := sha256.Sum256(ghOstBinary)
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "dmsx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "gh-ost-"+hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, ghOstBinary, 0o755); err != nil {
		return "", err
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		os.Remove(temporary)
		return "", err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return "", err
	}
	return path, nil
}
