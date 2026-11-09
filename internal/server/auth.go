package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"dmsx/internal/app"
)

const sessionCookie = "dmsx_session"

type session struct {
	issued time.Time
	until  time.Time
}

type sessions struct {
	mu   sync.Mutex
	open map[string]session
	// changedAt is when the operator password last changed; a session issued
	// before it is no longer valid.
	changedAt func(context.Context) time.Time
}

func newSessions(changedAt func(context.Context) time.Time) *sessions {
	return &sessions{open: map[string]session{}, changedAt: changedAt}
}

func (s *sessions) issue() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw[:])
	now := time.Now()
	s.mu.Lock()
	s.open[token] = session{issued: now, until: now.Add(12 * time.Hour)}
	s.mu.Unlock()
	return token, nil
}

func (s *sessions) valid(ctx context.Context, token string) bool {
	s.mu.Lock()
	current, ok := s.open[token]
	s.mu.Unlock()
	if !ok {
		return false
	}
	if time.Now().After(current.until) || current.issued.Before(s.changedAt(ctx)) {
		s.drop(token)
		return false
	}
	return true
}

func (s *sessions) drop(token string) {
	s.mu.Lock()
	delete(s.open, token)
	s.mu.Unlock()
}

func (s *sessions) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public(r) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !s.valid(r.Context(), cookie.Value) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要登录"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func public(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/favicon.svg" || r.URL.Path == "/app.css" || r.URL.Path == "/app.js" || r.URL.Path == "/api/health" || r.URL.Path == "/api/session"):
		return true
	case (r.Method == http.MethodPost && (r.URL.Path == "/api/session" || r.URL.Path == "/api/setup")) || (r.Method == http.MethodDelete && r.URL.Path == "/api/session"):
		return true
	default:
		return false
	}
}

func registerSession(mux *http.ServeMux, application *app.App, store *sessions) {
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		configured, err := application.OperatorConfigured(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		authenticated := err == nil && store.valid(r.Context(), cookie.Value)
		writeJSON(w, http.StatusOK, map[string]bool{"configured": configured, "authenticated": authenticated})
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := application.SetOperatorPassword(r.Context(), body.Password); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"configured": true})
	})
	mux.HandleFunc("POST /api/session", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := application.CheckOperatorPassword(r.Context(), body.Password); err != nil {
			if errors.Is(err, app.ErrPassword) {
				application.Audit(r.Context(), "session.refused", "wrong password from "+clientAddr(r))
			}
			writeError(w, err)
			return
		}
		application.Audit(r.Context(), "session.open", "signed in from "+clientAddr(r))
		token, err := store.issue()
		if err != nil {
			writeError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   12 * 3600,
		})
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	})
	mux.HandleFunc("DELETE /api/session", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			if store.valid(r.Context(), cookie.Value) {
				application.Audit(r.Context(), "session.close", "signed out from "+clientAddr(r))
			}
			store.drop(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
	})
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func authStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrPassword), errors.Is(err, app.ErrUnset):
		return http.StatusUnauthorized
	case errors.Is(err, app.ErrConfigured):
		return http.StatusConflict
	default:
		return 0
	}
}
