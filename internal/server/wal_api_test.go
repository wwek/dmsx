package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dmsx/internal/app"
)

func TestListWalSegmentsEmpty(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := New(application)
	cookie := signIn(t, handler)

	request := httptest.NewRequest(http.MethodGet, "/api/wal/segments", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
	var rows []any
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("want empty, got %v", rows)
	}
}

func TestListArchivePlansEmpty(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	handler := New(application)
	cookie := signIn(t, handler)

	request := httptest.NewRequest(http.MethodGet, "/api/archive/plans", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d %s", response.Code, response.Body.String())
	}
}
