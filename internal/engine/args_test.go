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
