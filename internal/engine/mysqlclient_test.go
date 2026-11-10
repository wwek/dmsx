package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A client that knows the public key flag accepts it, one that does not
// refuses to start. dmsx must never hand the flag to a client that would
// reject it, because that would break every dump on that client.
func TestMyClientArgsFollowTheClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake client is a shell script")
	}
	dir := t.TempDir()

	modern := filepath.Join(dir, "modern")
	if err := os.WriteFile(modern, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho \"unknown option '--get-server-public-key'\" >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DMSX_MYSQLDUMP", modern)
	publicKeySupport.Delete(mustResolve(t, modern))
	if args := myClientArgs(context.Background(), "mysqldump", false); strings.Join(args, " ") != publicKeyFlag {
		t.Fatalf("modern client args %v", args)
	}

	t.Setenv("DMSX_MYSQLDUMP", old)
	publicKeySupport.Delete(mustResolve(t, old))
	if args := myClientArgs(context.Background(), "mysqldump", false); len(args) != 0 {
		t.Fatalf("old client args %v", args)
	}
}

func TestMyClientArgsSkipMariaDB(t *testing.T) {
	if args := myClientArgs(context.Background(), "mariadb-dump", true); len(args) != 0 {
		t.Fatalf("maria args %v", args)
	}
}

func mustResolve(t *testing.T, bin string) string {
	t.Helper()
	path, err := look(bin)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
