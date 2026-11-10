package engine

import (
	"strings"
	"testing"
)

// The PostgreSQL logical backup is a custom format dump, and the schema
// include and the table exclude flags stay on that command.
func TestPostgresDumpArgsCarryTheFilters(t *testing.T) {
	src := Source{
		Engine: PostgreSQL, Host: "pg.example", Port: 5432, User: "postgres",
		Password: "pg-secret", Database: "app", Version: "16.1",
		IncludeSchemas:   []string{"public", "audit"},
		ExcludeTables:    []string{"logs"},
		ExcludeTableData: []string{"events"},
	}
	args := strings.Join(postgresDumpArgs(src), " ")
	for _, want := range []string{
		"-Fc", "--no-password", "-h pg.example", "-p 5432", "-U postgres", "-d app",
		"-n public", "-n audit", "--exclude-table=logs", "--exclude-table-data=events",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s in %s", want, args)
		}
	}
	if strings.Contains(args, "pg-secret") {
		t.Fatalf("the password reached the arguments: %s", args)
	}
}

// MongoDB is dumped as one gzip archive on stdout.
func TestMongoArgsAreOneGzipArchive(t *testing.T) {
	src := Source{
		Engine: MongoDB, Host: "mongo.example", Port: 27017, User: "root",
		Password: "mongo-secret", Database: "app",
		ExcludeTables: []string{"sessions"}, Parallel: 4,
	}
	dump := strings.Join(mongoDumpArgs(src), " ")
	for _, want := range []string{"--archive", "--gzip", "--excludeCollection=sessions", "--numParallelCollections=4"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("missing %s in %s", want, dump)
		}
	}
	restore := strings.Join(mongoRestoreArgs(src), " ")
	for _, want := range []string{"--archive", "--gzip", "--drop"} {
		if !strings.Contains(restore, want) {
			t.Fatalf("missing %s in %s", want, restore)
		}
	}
	if !strings.Contains(dump, "--uri=mongodb://") {
		t.Fatalf("the dump does not name the server: %s", dump)
	}
	// The collection parallelism is clamped to what mongodump takes.
	src.Parallel = 64
	if strings.Contains(strings.Join(mongoDumpArgs(src), " "), "--numParallelCollections=64") {
		t.Fatal("the collection parallelism was not clamped")
	}
}
