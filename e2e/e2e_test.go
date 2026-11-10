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
}

func TestMySQLOnlineDDL(t *testing.T) {
	if _, err := exec.LookPath("gh-ost"); err != nil {
		t.Skip("gh-ost is not installed")
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

func waitDDL(t *testing.T, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
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
