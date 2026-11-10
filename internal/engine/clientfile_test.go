package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The password goes into a client file that only its owner can read, and not
// into the process arguments where anyone can read it.
func TestClientFilesHoldThePasswordWithTightPermissions(t *testing.T) {
	src := Source{Engine: MySQL, Host: "db.example", Port: 3306, User: "root",
		Password: "secret-pass", Database: "shop"}

	cnf, err := writeMyCNF(src, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cnf.remove()
	mysqlFile := filepath.Join(cnf.dir, ".my.cnf")
	assertPrivateFile(t, mysqlFile, cnf.dir, "secret-pass")

	pass, err := writePGPass(src)
	if err != nil {
		t.Fatal(err)
	}
	defer pass.remove()
	assertPrivateFile(t, pass.path, pass.dir, "secret-pass")
}

func assertPrivateFile(t *testing.T, path, directory, secret string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s has mode %v", path, info.Mode().Perm())
	}
	dir, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("%s has mode %v", directory, dir.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), secret) {
		t.Fatalf("%s does not hold the password: %s", path, body)
	}
}
