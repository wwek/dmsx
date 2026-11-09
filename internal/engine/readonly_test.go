package engine

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTool installs a script that records its arguments and stdin.
func fakeTool(t *testing.T, name string) (args, stdin string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script tool stand-in")
	}
	dir := t.TempDir()
	args = filepath.Join(dir, "args")
	stdin = filepath.Join(dir, "stdin")
	script := "#!/bin/sh\necho \"$@\" > " + args + "\ncat > " + stdin + "\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DMSX_"+strings.ToUpper(name), path)
	return args, stdin
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestMySQLBackupUserGoesThroughStdin(t *testing.T) {
	argsPath, stdinPath := fakeTool(t, "mysql")
	src := Source{Engine: MySQL, Host: "db", Port: 3306, User: "root", Password: "rootpw", Database: "shop"}
	user, err := CreateBackupUser(context.Background(), src, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(user.Username, "dmsx_ro_") || len(user.Password) != 32 {
		t.Fatalf("user %+v", user)
	}
	args, sql := read(t, argsPath), read(t, stdinPath)
	if strings.Contains(args, user.Password) || strings.Contains(args, "rootpw") {
		t.Fatalf("password in arguments: %s", args)
	}
	if !strings.Contains(sql, "GRANT SELECT, SHOW VIEW, LOCK TABLES, TRIGGER, EVENT ON `shop`.*") {
		t.Fatalf("sql %s", sql)
	}
}

func TestPostgresBackupRoleSendsVerifier(t *testing.T) {
	argsPath, stdinPath := fakeTool(t, "psql")
	src := Source{Engine: PostgreSQL, Host: "db", Port: 5432, User: "postgres", Password: "pw", Database: "shop"}
	user, err := CreateBackupUser(context.Background(), src, false)
	if err != nil {
		t.Fatal(err)
	}
	args, sql := read(t, argsPath), read(t, stdinPath)
	if strings.Contains(args, user.Password) || strings.Contains(sql, user.Password) {
		t.Fatal("plaintext password left dmsx")
	}
	if !strings.Contains(sql, "PASSWORD 'SCRAM-SHA-256$4096:") || !strings.Contains(sql, "GRANT CONNECT ON DATABASE \"shop\"") {
		t.Fatalf("sql %s", sql)
	}
	if !strings.Contains(args, "ON_ERROR_STOP=1") || !strings.Contains(args, "--single-transaction") {
		t.Fatalf("args %s", args)
	}

	if _, err := CreateBackupUser(context.Background(), src, true); err != nil {
		t.Fatal(err)
	}
	if sql := read(t, stdinPath); !strings.Contains(sql, "LOGIN REPLICATION PASSWORD") || strings.Contains(sql, "GRANT") {
		t.Fatalf("replication sql %s", sql)
	}
}

func TestMongoBackupUserSignsInInsideScript(t *testing.T) {
	argsPath, stdinPath := fakeTool(t, "mongosh")
	src := Source{Engine: MongoDB, Host: "db", Port: 27017, User: "admin", Password: "secret\"pw", Database: "shop"}
	if _, err := CreateBackupUser(context.Background(), src, false); err != nil {
		t.Fatal(err)
	}
	args, script := read(t, argsPath), read(t, stdinPath)
	if strings.Contains(args, "secret") {
		t.Fatalf("password in arguments: %s", args)
	}
	if !strings.Contains(script, `home.auth("admin", "secret\"pw")`) || !strings.Contains(script, `{role: "read", db: "shop"}`) {
		t.Fatalf("script %s", script)
	}
}

func TestScramVerifierMatchesItsSalt(t *testing.T) {
	verifier, err := scramVerifier("pencil")
	if err != nil {
		t.Fatal(err)
	}
	head, keys, _ := strings.Cut(strings.TrimPrefix(verifier, "SCRAM-SHA-256$"), "$")
	_, saltText, _ := strings.Cut(head, ":")
	storedText, serverText, _ := strings.Cut(keys, ":")
	salt, _ := base64.StdEncoding.DecodeString(saltText)
	salted, _ := pbkdf2.Key(sha256.New, "pencil", salt, 4096, 32)
	clientKey := hmacSHA256(salted, "Client Key")
	stored := sha256.Sum256(clientKey)
	if base64.StdEncoding.EncodeToString(stored[:]) != storedText {
		t.Fatal("stored key mismatch")
	}
	if !hmac.Equal([]byte(base64.StdEncoding.EncodeToString(hmacSHA256(salted, "Server Key"))), []byte(serverText)) {
		t.Fatal("server key mismatch")
	}
}
