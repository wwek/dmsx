//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMySQLBackupArchiveAndColdQuery(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	archives := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	signIn(t)

	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	storage := postJSON(t, "/api/storages", map[string]any{
		"name": "local", "type": "LOCAL", "settings": map[string]string{"path": archives},
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)
	postOK(t, "/api/storages/"+storage["id"].(string)+"/test", nil)

	// A table large enough to cross several archive batches, so the primary
	// key cursor has to carry over from one batch to the next.
	mysqlExec(t, `CREATE TABLE events (
		id BIGINT NOT NULL PRIMARY KEY,
		kind VARCHAR(32) NOT NULL,
		amount INT NOT NULL,
		create_time DATETIME NOT NULL
	) ENGINE=InnoDB`)
	mysqlExec(t, `SET SESSION cte_max_recursion_depth = 500000;
		INSERT INTO events (id, kind, amount, create_time)
		WITH RECURSIVE seq (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 12000)
		SELECT n, IF(n % 2 = 0, 'even', 'odd'), n, '2024-03-01 00:00:00' FROM seq`)

	config := postJSON(t, "/api/backups/configs", map[string]any{
		"name": "nightly", "databaseId": database["id"], "storageId": storage["id"],
		"method":    "logical",
		"schedule":  map[string]string{"type": "HOURLY"},
		"retention": map[string]any{"kind": "COUNT", "count": 7},
	})
	run := postJSON(t, "/api/backups/configs/"+config["id"].(string)+"/run", nil)
	finished := waitRun(t, run["id"].(string))
	if finished["status"] != "success" {
		t.Fatalf("backup %+v", finished)
	}
	if bytes, _ := finished["bytes"].(float64); bytes < 200 {
		t.Fatalf("backup too small: %+v", finished)
	}
	body := get(t, "/api/backups/runs/"+finished["id"].(string)+"/download")
	if !bytes.Contains(body, []byte("old-a")) || !bytes.Contains(body, []byte("CREATE TABLE")) {
		t.Fatalf("dump missing table data:\n%s", truncate(body))
	}

	job := postJSON(t, "/api/archive/jobs", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "create_time < '2025-01-01'", "year": 2024,
		"batchSize": 5000, "deleteAfter": true,
	})
	archived := waitArchive(t, job["id"].(string))
	if archived["status"] != "success" {
		t.Fatalf("archive %+v", archived)
	}
	if archived["rowsArchived"].(float64) != 2 || archived["rowsDeleted"].(float64) != 2 {
		t.Fatalf("archive counts %+v", archived)
	}
	objectKey, _ := archived["objectKey"].(string)
	if !strings.HasPrefix(objectKey, "dms_archives/shop/orders/2024_archive_") || !strings.HasSuffix(objectKey, ".parquet") {
		t.Fatalf("object key %v", archived["objectKey"])
	}
	left := mysqlCount(t)
	if left != "1" {
		t.Fatalf("rows left in mysql: %s", left)
	}

	rows := postColdRows(t, "/api/cold-query", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "id = 1", "limit": 100,
	})
	if len(rows) != 1 || fmt.Sprint(rows[0]["note"]) != "old-a" {
		t.Fatalf("cold query %#v", rows)
	}
	rejected := postStatus(t, "/api/cold-query", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "id = 1; DROP TABLE orders",
	})
	if rejected != http.StatusBadRequest {
		t.Fatalf("filter status %d", rejected)
	}

	events := postJSON(t, "/api/archive/jobs", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "events", "where": "create_time < '2025-01-01'", "year": 2024,
		"batchSize": 5000, "deleteAfter": true,
	})
	archivedEvents := waitArchive(t, events["id"].(string))
	if archivedEvents["status"] != "success" {
		t.Fatalf("archive %+v", archivedEvents)
	}
	if archivedEvents["rowsArchived"].(float64) != 12000 || archivedEvents["rowsDeleted"].(float64) != 12000 {
		t.Fatalf("archive counts %+v", archivedEvents)
	}
	keys, _ := archivedEvents["objectKeys"].([]any)
	if len(keys) != 3 {
		t.Fatalf("expected one object per batch, got %d: %+v", len(keys), archivedEvents)
	}
	objects, err := filepath.Glob(filepath.Join(archives, "dms_archives", "shop", "events", "*.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 3 {
		t.Fatalf("expected 3 parquet objects, got %d: %v", len(objects), objects)
	}
	if left := mysqlScalar(t, "SELECT COUNT(*) FROM shop.events"); left != "0" {
		t.Fatalf("rows left in mysql: %s", left)
	}
	eventRows := postColdRows(t, "/api/cold-query", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "events", "where": "id = 11999", "limit": 100,
	})
	if len(eventRows) != 1 || fmt.Sprint(eventRows[0]["kind"]) != "odd" {
		t.Fatalf("cold query over the batched archive %#v", eventRows)
	}
}

func TestMySQLOnlineDDL(t *testing.T) {
	if ghOstPath() == "" {
		t.Fatal("gh-ost is required for the online DDL test; install it or point DMSX_GH_OST at it")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	sessionCookie = ""
	signIn(t)

	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)
	run := postJSON(t, "/api/ddl/runs", map[string]any{
		"databaseId": database["id"],
		"statement":  "ALTER TABLE orders ADD COLUMN extra varchar(32)",
		"tool":       "gh-ost",
		"chunkSize":  1000,
		"execute":    false,
	})
	finished := waitDDL(t, run["id"].(string))
	if finished["status"] != "success" {
		t.Fatalf("ddl %+v", finished)
	}
	if finished["execute"] == true {
		t.Fatalf("dry run was stored as execute: %+v", finished)
	}
	if mysqlScalar(t, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='shop' AND table_name='orders' AND column_name='extra'") != "0" {
		t.Fatal("dry run changed the table")
	}
}

func TestMySQLOnlineDDLCutoverOnLargeTable(t *testing.T) {
	if ghOstPath() == "" {
		t.Fatal("gh-ost is required for the online DDL test; install it or point DMSX_GH_OST at it")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	sessionCookie = ""
	signIn(t)

	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)

	mysqlExec(t, `CREATE TABLE ddl_big (
		id BIGINT NOT NULL PRIMARY KEY,
		payload VARCHAR(64) NOT NULL,
		created_at DATETIME NOT NULL
	) ENGINE=InnoDB`)
	mysqlExec(t, `SET SESSION cte_max_recursion_depth = 500000;
		INSERT INTO ddl_big (id, payload, created_at)
		WITH RECURSIVE seq (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 120000)
		SELECT n, CONCAT('row-', n), '2024-01-01 00:00:00' FROM seq`)
	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop.ddl_big"); rows != "120000" {
		t.Fatalf("fixture rows %s", rows)
	}

	run := postJSON(t, "/api/ddl/runs", map[string]any{
		"databaseId": database["id"],
		"statement":  "ALTER TABLE ddl_big ADD COLUMN extra varchar(32)",
		"tool":       "gh-ost",
		"chunkSize":  1000,
		"execute":    true,
	})
	finished := waitDDL(t, run["id"].(string))
	if finished["status"] != "success" {
		t.Fatalf("ddl %+v", finished)
	}
	if finished["execute"] != true {
		t.Fatalf("run was not an execute: %+v", finished)
	}
	if copied, _ := finished["copied"].(float64); copied < 120000 {
		t.Fatalf("gh-ost copied %v of 120000 rows: %+v", finished["copied"], finished)
	}
	if column := mysqlScalar(t, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='shop' AND table_name='ddl_big' AND column_name='extra'"); column != "1" {
		t.Fatal("the new column is missing after the cut-over")
	}
	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop.ddl_big"); rows != "120000" {
		t.Fatalf("rows after the cut-over %s", rows)
	}
	if wrong := mysqlScalar(t, "SELECT COUNT(*) FROM shop.ddl_big WHERE payload <> CONCAT('row-', id)"); wrong != "0" {
		t.Fatalf("%s rows lost their payload", wrong)
	}
	if ghost := mysqlScalar(t, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='shop' AND table_name='_ddl_big_gho'"); ghost != "0" {
		t.Fatal("the ghost table was left behind")
	}
}

// ghOstPath mirrors the lookup the server does, so the test never passes by
// skipping a tool the server would also be missing.
func ghOstPath() string {
	if env := os.Getenv("DMSX_GH_OST"); env != "" {
		return env
	}
	path, err := exec.LookPath("gh-ost")
	if err != nil {
		return ""
	}
	return path
}

func TestMySQLBackupRestoreRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	archives := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	sessionCookie = ""
	signIn(t)

	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	storage := postJSON(t, "/api/storages", map[string]any{
		"name": "local", "type": "LOCAL", "settings": map[string]string{"path": archives},
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)
	postOK(t, "/api/storages/"+storage["id"].(string)+"/test", nil)

	mysqlExec(t, `CREATE TABLE events (
		id BIGINT NOT NULL PRIMARY KEY,
		kind VARCHAR(32) NOT NULL,
		amount INT NOT NULL,
		create_time DATETIME NOT NULL
	) ENGINE=InnoDB`)
	mysqlExec(t, `SET SESSION cte_max_recursion_depth = 500000;
		INSERT INTO events (id, kind, amount, create_time)
		WITH RECURSIVE seq (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 12000)
		SELECT n, IF(n % 2 = 0, 'even', 'odd'), n, '2024-03-01 00:00:00' FROM seq`)
	mysqlExec(t, `CREATE TABLE secret_notes (
		id INT NOT NULL PRIMARY KEY,
		body VARCHAR(64) NOT NULL
	) ENGINE=InnoDB`)
	mysqlExec(t, `INSERT INTO secret_notes (id, body) VALUES (1, 'classified')`)

	config := postJSON(t, "/api/backups/configs", map[string]any{
		"name": "nightly", "databaseId": database["id"], "storageId": storage["id"],
		"method":        "logical",
		"schedule":      map[string]string{"type": "HOURLY"},
		"retention":     map[string]any{"kind": "COUNT", "count": 7},
		"encrypted":     true,
		"excludeTables": []string{"secret_notes"},
	})
	run := postJSON(t, "/api/backups/configs/"+config["id"].(string)+"/run", nil)
	finished := waitRun(t, run["id"].(string))
	if finished["status"] != "success" {
		t.Fatalf("backup %+v", finished)
	}
	if finished["encrypted"] != true {
		t.Fatalf("backup was not encrypted: %+v", finished)
	}

	// The stored object is the encrypted stream, not the plain SQL.
	fileName, _ := finished["fileName"].(string)
	stored, err := os.ReadFile(filepath.Join(archives, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(stored, []byte("DMSXBK01")) {
		t.Fatalf("the stored object is not an encrypted stream: %q", truncate(stored))
	}
	if bytes.Contains(stored, []byte("CREATE TABLE")) {
		t.Fatal("the stored object still holds plain SQL")
	}

	mysqlExec(t, "CREATE DATABASE shop_restored")
	restore := postJSON(t, "/api/backups/runs/"+finished["id"].(string)+"/restore", map[string]any{
		"host": "127.0.0.1", "port": 13306, "username": "root", "password": "dmsx-e2e",
		"database": "shop_restored", "verify": true,
	})
	restored := waitRestore(t, restore["id"].(string))
	if restored["status"] != "success" {
		t.Fatalf("restore %+v", restored)
	}
	if report, _ := restored["report"].(string); report != "tables=2" {
		t.Fatalf("restore report %q", report)
	}

	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop_restored.orders"); rows != "3" {
		t.Fatalf("restored orders %s", rows)
	}
	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop_restored.events"); rows != "12000" {
		t.Fatalf("restored events %s", rows)
	}
	if wrong := mysqlScalar(t, "SELECT COUNT(*) FROM shop_restored.events WHERE kind <> IF(id % 2 = 0, 'even', 'odd')"); wrong != "0" {
		t.Fatalf("%s restored rows lost their value", wrong)
	}
	if tables := mysqlScalar(t, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='shop_restored' AND table_name='secret_notes'"); tables != "0" {
		t.Fatal("the excluded table was restored")
	}

	// The target password was used for that restore and was not kept.
	if strings.Contains(string(get(t, "/api/backups/restores")), "dmsx-e2e") {
		t.Fatal("the restore kept the target password")
	}

	// The upstream reference also proves a backup taken by a limited login.
	// dmsx creates that login and switches the connection over to it.
	postJSON(t, "/api/databases/"+database["id"].(string)+"/backup-user", map[string]any{"replication": false})
	limited := postJSON(t, "/api/backups/configs/"+config["id"].(string)+"/run", nil)
	limitedFinished := waitRun(t, limited["id"].(string))
	if limitedFinished["status"] != "success" {
		t.Fatalf("backup with the limited user %+v", limitedFinished)
	}
	mysqlExec(t, "CREATE DATABASE shop_limited")
	limitedRestore := postJSON(t, "/api/backups/runs/"+limitedFinished["id"].(string)+"/restore", map[string]any{
		"host": "127.0.0.1", "port": 13306, "username": "root", "password": "dmsx-e2e",
		"database": "shop_limited", "verify": true,
	})
	limitedRestored := waitRestore(t, limitedRestore["id"].(string))
	if limitedRestored["status"] != "success" {
		t.Fatalf("restore of the limited user backup %+v", limitedRestored)
	}
	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop_limited.events"); rows != "12000" {
		t.Fatalf("restored events from the limited user backup %s", rows)
	}
	if rows := mysqlScalar(t, "SELECT COUNT(*) FROM shop_limited.orders"); rows != "3" {
		t.Fatalf("restored orders from the limited user backup %s", rows)
	}
}

func TestMySQLArchiveToS3AndColdQuery(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	sessionCookie = ""
	signIn(t)

	bucket := newS3Server(t)
	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	storage := postJSON(t, "/api/storages", map[string]any{
		"name": "bucket", "type": "S3",
		"settings": map[string]any{
			"bucket": "archives", "region": "us-east-1", "endpoint": bucket.endpoint(),
			"accessKey": "dmsx", "secretKey": "dmsx-secret", "prefix": "dmsx",
		},
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)
	postOK(t, "/api/storages/"+storage["id"].(string)+"/test", nil)

	job := postJSON(t, "/api/archive/jobs", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "create_time < '2025-01-01'", "year": 2024,
		"batchSize": 5000, "deleteAfter": true,
	})
	archived := waitArchive(t, job["id"].(string))
	if archived["status"] != "success" {
		t.Fatalf("archive %+v\nrequests %v", archived, bucket.trace())
	}
	if archived["rowsArchived"].(float64) != 2 || archived["rowsDeleted"].(float64) != 2 {
		t.Fatalf("archive counts %+v\nrequests %v", archived, bucket.trace())
	}

	// The upload landed under the storage prefix, and the object that was
	// read back for the MD5 check is the one that was written.
	keys := bucket.keys("archives")
	if len(keys) != 1 {
		t.Fatalf("objects %v\nrequests %v", keys, bucket.trace())
	}
	if !strings.HasPrefix(keys[0], "dmsx/dms_archives/shop/orders/2024_archive_") || !strings.HasSuffix(keys[0], ".parquet") {
		t.Fatalf("object key %v\nrequests %v", keys[0], bucket.trace())
	}
	if left := mysqlCount(t); left != "1" {
		t.Fatalf("rows left in mysql: %s\nrequests %v", left, bucket.trace())
	}

	// The cold query reads the same prefixed object back.
	rows := postColdRows(t, "/api/cold-query", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "id = 1", "limit": 100,
	})
	if len(rows) != 1 || fmt.Sprint(rows[0]["note"]) != "old-a" {
		t.Fatalf("cold query %#v\nrequests %v", rows, bucket.trace())
	}
}

func TestMySQLArchiveToQiniuAndColdQuery(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is required")
	}
	if err := compose("up", "-d", "--wait"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = compose("down", "-v") })

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	bin := filepath.Join(t.TempDir(), "dmsx")
	build := exec.Command("go", "build", "-o", bin, "./cmd/dmsx")
	build.Dir = root
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	server := exec.Command(bin)
	server.Env = append(os.Environ(), "DMSX_ADDR=127.0.0.1:18080", "DMSX_DATA="+data)
	server.Stdout, server.Stderr = os.Stdout, os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})
	waitHealthy(t, "http://127.0.0.1:18080/api/health")
	sessionCookie = ""
	signIn(t)

	bucket := newS3Server(t)
	database := postJSON(t, "/api/databases", map[string]any{
		"name": "shop", "engine": "mysql", "host": "127.0.0.1", "port": 13306,
		"username": "root", "password": "dmsx-e2e", "database": "shop", "version": "8.4",
	})
	storage := postJSON(t, "/api/storages", map[string]any{
		"name": "qiniu", "type": "QINIU",
		"settings": map[string]any{
			"bucket": "qiniu-archive", "region": "cn-east-1", "endpoint": bucket.endpoint(),
			"accessKey": "dmsx", "secretKey": "dmsx-secret", "prefix": "qi",
		},
	})
	postOK(t, "/api/databases/"+database["id"].(string)+"/test", nil)
	postOK(t, "/api/storages/"+storage["id"].(string)+"/test", nil)

	job := postJSON(t, "/api/archive/jobs", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "create_time < '2025-01-01'", "year": 2024,
		"batchSize": 5000, "deleteAfter": true,
	})
	archived := waitArchive(t, job["id"].(string))
	if archived["status"] != "success" {
		t.Fatalf("archive %+v\nrequests %v", archived, bucket.trace())
	}
	if archived["rowsArchived"].(float64) != 2 || archived["rowsDeleted"].(float64) != 2 {
		t.Fatalf("archive counts %+v\nrequests %v", archived, bucket.trace())
	}

	keys := bucket.keys("qiniu-archive")
	if len(keys) != 1 {
		t.Fatalf("objects %v\nrequests %v", keys, bucket.trace())
	}
	if !strings.HasPrefix(keys[0], "qi/dms_archives/shop/orders/2024_archive_") || !strings.HasSuffix(keys[0], ".parquet") {
		t.Fatalf("object key %v\nrequests %v", keys[0], bucket.trace())
	}
	if left := mysqlCount(t); left != "1" {
		t.Fatalf("rows left in mysql: %s\nrequests %v", left, bucket.trace())
	}

	rows := postColdRows(t, "/api/cold-query", map[string]any{
		"databaseId": database["id"], "storageId": storage["id"],
		"table": "orders", "where": "id = 1", "limit": 100,
	})
	if len(rows) != 1 || fmt.Sprint(rows[0]["note"]) != "old-a" {
		t.Fatalf("cold query %#v\nrequests %v", rows, bucket.trace())
	}
}

func waitRestore(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range getArray(t, "/api/backups/restores") {
			if row["id"] == id && row["status"] != "running" {
				return row
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("restore did not finish")
	return nil
}

func waitDDL(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range getArray(t, "/api/ddl/runs") {
			if row["id"] == id && row["status"] != "running" {
				return row
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("ddl did not finish")
	return nil
}

func mysqlScalar(t *testing.T, query string) string {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "-p", "dmsx-e2e", "exec", "-T", "mysql",
		"mysql", "-uroot", "-pdmsx-e2e", "-N", "-e", query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mysql: %v\n%s", err, out)
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return string(bytes.TrimSpace(lines[len(lines)-1]))
}

func mysqlExec(t *testing.T, query string) {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "-p", "dmsx-e2e", "exec", "-T", "mysql",
		"mysql", "-uroot", "-pdmsx-e2e", "shop", "-e", query)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mysql: %v\n%s", err, out)
	}
}

func compose(args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose", "-f", "docker-compose.yml", "-p", "dmsx-e2e"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func waitHealthy(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("dmsx did not become healthy")
}

func waitRun(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range getArray(t, "/api/backups/runs") {
			if row["id"] == id && row["status"] != "running" {
				return row
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("backup did not finish")
	return nil
}

func waitArchive(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, row := range getArray(t, "/api/archive/jobs") {
			if row["id"] == id && row["status"] != "running" {
				return row
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("archive did not finish")
	return nil
}

func mysqlCount(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", "docker-compose.yml", "-p", "dmsx-e2e", "exec", "-T", "mysql",
		"mysql", "-uroot", "-pdmsx-e2e", "-N", "-e", "SELECT COUNT(*) FROM shop.orders")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mysql count: %v\n%s", err, out)
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	return string(bytes.TrimSpace(lines[len(lines)-1]))
}

var sessionCookie string

func signIn(t *testing.T) {
	t.Helper()
	postJSON(t, "/api/setup", map[string]any{"password": "dmsx-e2e-pass"})
	postJSON(t, "/api/session", map[string]any{"password": "dmsx-e2e-pass"})
	if sessionCookie == "" {
		t.Fatal("session cookie missing")
	}
}

func postJSON(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	raw := postStatusBody(t, path, body)
	if raw.code >= 300 {
		t.Fatalf("POST %s %d %s", path, raw.code, raw.body)
	}
	var out map[string]any
	if err := json.Unmarshal(raw.body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func postColdRows(t *testing.T, path string, body any) []map[string]any {
	t.Helper()
	raw := postStatusBody(t, path, body)
	if raw.code >= 300 {
		t.Fatalf("POST %s %d %s", path, raw.code, raw.body)
	}
	var envelope struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(raw.body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Rows
}

func postOK(t *testing.T, path string, body any) {
	t.Helper()
	raw := postStatusBody(t, path, body)
	if raw.code >= 300 {
		t.Fatalf("POST %s %d %s", path, raw.code, raw.body)
	}
}

func postStatus(t *testing.T, path string, body any) int {
	t.Helper()
	return postStatusBody(t, path, body).code
}

type responseBody struct {
	code int
	body []byte
}

func postStatusBody(t *testing.T, path string, body any) responseBody {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18080"+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if sessionCookie != "" {
		request.AddCookie(&http.Cookie{Name: "dmsx_session", Value: sessionCookie})
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if cookie := sessionValue(response); cookie != "" {
		sessionCookie = cookie
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return responseBody{code: response.StatusCode, body: payload}
}

func sessionValue(response *http.Response) string {
	for _, cookie := range response.Cookies() {
		if cookie.Name == "dmsx_session" {
			return cookie.Value
		}
	}
	return ""
}

func get(t *testing.T, path string) []byte {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:18080"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sessionCookie != "" {
		request.AddCookie(&http.Cookie{Name: "dmsx_session", Value: sessionCookie})
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode >= 300 {
		t.Fatalf("GET %s %d %s", path, response.StatusCode, payload)
	}
	return payload
}

func getArray(t *testing.T, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(get(t, path), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func truncate(body []byte) string {
	if len(body) > 500 {
		return string(body[:500])
	}
	return string(body)
}
