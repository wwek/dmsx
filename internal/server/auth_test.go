package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dmsx/internal/app"
)

func TestResetPasswordEndsOpenSessions(t *testing.T) {
	application, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	handler := New(application)

	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := call(http.MethodGet, "/api/session", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"dev"`) {
		t.Fatalf("session version %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPost, "/api/setup", `{"password":"first-password"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("setup %d %s", rec.Code, rec.Body)
	}
	login := call(http.MethodPost, "/api/session", `{"password":"first-password"}`, nil)
	cookies := login.Result().Cookies()
	if login.Code != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login %d", login.Code)
	}
	if rec := call(http.MethodGet, "/api/databases", "", cookies[0]); rec.Code != http.StatusOK {
		t.Fatalf("signed-in request %d", rec.Code)
	}
	time.Sleep(5 * time.Millisecond)
	if err := application.ResetOperatorPassword(context.Background(), "second-password"); err != nil {
		t.Fatal(err)
	}
	if rec := call(http.MethodGet, "/api/databases", "", cookies[0]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("session survived the reset: %d", rec.Code)
	}
	if rec := call(http.MethodGet, "/api/audit", "", cookies[0]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("audit readable without a session: %d", rec.Code)
	}
}
