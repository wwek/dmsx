package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"dmsx/internal/app"
)

func TestCreateDatabaseAndStorage(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := New(application)
	cookie := signIn(t, handler)

	body := bytes.NewBufferString(`{"name":"local","type":"LOCAL","settings":{"path":"` + filepath.Join(t.TempDir(), "archives") + `"}}`)
	request := httptest.NewRequest(http.MethodPost, "/api/storages", body)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("storage %d %s", response.Code, response.Body.String())
	}

	body = bytes.NewBufferString(`{"name":"shop","engine":"mysql","host":"127.0.0.1","port":3306,"username":"root","password":"secret","database":"shop"}`)
	request = httptest.NewRequest(http.MethodPost, "/api/databases", body)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("database %d %s", response.Code, response.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["password"] != nil && created["password"] != "" {
		t.Fatalf("password leaked: %v", created["password"])
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("id=\"root\"")) || !bytes.Contains(response.Body.Bytes(), []byte("/assets/")) {
		t.Fatalf("index %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("<svg")) {
		t.Fatalf("favicon %d %s", response.Code, response.Body.String())
	}
}

func TestAPIRequiresSignIn(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := New(application)
	request := httptest.NewRequest(http.MethodGet, "/api/databases", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", response.Code)
	}
}

func signIn(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	response := postJSON(t, handler, "/api/setup", `{"password":"dmsx-test-pass"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("setup %d %s", response.Code, response.Body.String())
	}
	response = postJSON(t, handler, "/api/session", `{"password":"dmsx-test-pass"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("session %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "dmsx_session" {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func postJSON(t *testing.T, handler http.Handler, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
