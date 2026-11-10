package onlineddl

import (
	"strings"
	"testing"
)

func ddlRequest() Request {
	return Request{
		Host: "db.example", Port: 3306, User: "root", Password: "ddl-secret-pass",
		Database: "shop", Statement: "ALTER TABLE orders ADD COLUMN a int", ChunkSize: 1000,
	}
}

// The cut-over must happen only when the operator asked for it. gh-ost reads
// --execute as that request, and pt-online-schema-change reads --execute
// against --dry-run.
func TestCutOverOnlyOnRequest(t *testing.T) {
	req := ddlRequest()

	dry := ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost")
	if hasArg(dry, "--execute") {
		t.Fatalf("a dry run asked gh-ost for the cut-over: %v", dry)
	}
	req.Execute = true
	cutover := ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost")
	if !hasArg(cutover, "--execute") {
		t.Fatalf("the cut-over was not asked for: %v", cutover)
	}

	dry = ptArgs(ddlRequest(), "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf")
	if !hasArg(dry, "--dry-run") || hasArg(dry, "--execute") {
		t.Fatalf("a dry run did not ask for a dry run: %v", dry)
	}
	cutover = ptArgs(ddlRequest2(), "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf")
	if !hasArg(cutover, "--execute") || hasArg(cutover, "--dry-run") {
		t.Fatalf("the cut-over was not asked for: %v", cutover)
	}
}

func ddlRequest2() Request {
	req := ddlRequest()
	req.Execute = true
	return req
}

// The password belongs in the defaults file and not in the process
// arguments, where anyone can read it.
func TestToolArgumentsHoldNoPassword(t *testing.T) {
	req := ddlRequest()
	req.Execute = true
	for _, args := range [][]string{
		ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost"),
		ptArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf"),
	} {
		for _, arg := range args {
			if strings.Contains(arg, req.Password) {
				t.Fatalf("the password reached the arguments: %v", args)
			}
		}
	}
}

// The port check is dropped only when the caller asked for it, and only for
// gh-ost, which is the tool that has the check.
func TestPortValidationIsSkippedOnRequestOnly(t *testing.T) {
	req := ddlRequest()
	if hasArg(ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost"), "--skip-port-validation") {
		t.Fatal("the port check was dropped without a request")
	}
	req.SkipPortValidation = true
	if !hasArg(ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost"), "--skip-port-validation") {
		t.Fatal("the port check was not dropped on request")
	}
	req.SkipPortValidation = true
	if hasArg(ptArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf"), "--skip-port-validation") {
		t.Fatal("pt-online-schema-change was given a flag it does not know")
	}
}

func TestToolArgumentsNameTheStatementAndTheServer(t *testing.T) {
	req := ddlRequest()
	gh := strings.Join(ghOstArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf", "/tmp/ghost"), " ")
	for _, want := range []string{"--database=shop", "--table=orders", "--alter=ADD COLUMN a int", "--conf=/tmp/client.cnf", "--chunk-size=1000"} {
		if !strings.Contains(gh, want) {
			t.Fatalf("missing %s in %s", want, gh)
		}
	}
	pt := strings.Join(ptArgs(req, "shop", "orders", "ADD COLUMN a int", "/tmp/client.cnf"), " ")
	for _, want := range []string{"--alter ADD COLUMN a int", "--defaults-file=/tmp/client.cnf", "h=db.example,P=3306,D=shop,t=orders", "--chunk-size 1000"} {
		if !strings.Contains(pt, want) {
			t.Fatalf("missing %s in %s", want, pt)
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
