package engine

import (
	"strings"
	"testing"
)

func TestMySQLDumpArgsOmitPassword(t *testing.T) {
	args := mysqlDumpArgs(Source{
		Database:        "shop",
		SSL:             false,
		IncludeTriggers: false,
		ExcludeTables:   []string{"audit"},
	}, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--single-transaction", "--quick", "--no-tablespaces", "--skip-triggers", "--ignore-table=shop.audit", "--set-gtid-purged=OFF", "shop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "password") {
		t.Fatal("password leaked into arguments")
	}
}

func TestPostgresRestoreArgsSkipOwnerAndACL(t *testing.T) {
	args := postgresRestoreArgs(Source{Host: "db.example", Port: 5432, User: "restore", Database: "shop", Parallel: 4}, "/tmp/toc.list", "/tmp/backup.dump")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--no-owner", "--no-acl", "-j 4", "-L /tmp/toc.list", "/tmp/backup.dump", "-d shop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--single-transaction") {
		t.Fatal("parallel restore must not use one transaction")
	}
	single := strings.Join(postgresRestoreArgs(Source{Host: "db", Port: 5432, User: "u", Database: "shop"}, "", ""), " ")
	if !strings.Contains(single, "--single-transaction") || strings.Contains(single, "-j") {
		t.Fatalf("single job: %s", single)
	}
}

func TestFilterRestoreTOCDropsExtensionsAndUserMappings(t *testing.T) {
	in := "; Archive\n3420; 0 0 EXTENSION - uuid-ossp\n3462; 0 0 COMMENT - EXTENSION \"uuid-ossp\"\n18239; 0 0 USER MAPPING - app SERVER remote\n215; 1259 16390 TABLE public orders\n"
	got := filterRestoreTOC(in)
	if strings.Contains(strings.ToUpper(got), " EXTENSION ") || strings.Contains(strings.ToUpper(got), " USER MAPPING ") {
		t.Fatalf("filtered:\n%s", got)
	}
	if !strings.Contains(got, "TABLE public orders") || !strings.Contains(got, "; Archive") {
		t.Fatalf("kept lines missing:\n%s", got)
	}
}

func TestPhysicalArgs(t *testing.T) {
	if got := physicalCodec("gzip"); got != "server-gzip:6" {
		t.Fatal(got)
	}
	if got := postgresCompressArgs("15.4"); strings.Join(got, " ") != "-Z 5" {
		t.Fatal(got)
	}
	if got := postgresCompressArgs("16.1"); strings.Join(got, " ") != "--compress=zstd:5" {
		t.Fatal(got)
	}
}
