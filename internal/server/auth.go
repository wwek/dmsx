package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"dmsx/internal/app"
	"dmsx/internal/version"
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

// requireAuth rejects API calls without a live session cookie. Static console
// files and the session endpoints stay public.
func (s *sessions) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if publicPath(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		cookie, err := c.Cookie(sessionCookie)
		if err != nil || !s.valid(c.Request.Context(), cookie) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要登录"})
			return
		}
		c.Next()
	}
}

func publicPath(method, path string) bool {
	switch {
	case method == http.MethodGet && (path == "/" || path == "/favicon.svg" || path == "/api/health" || path == "/api/session"):
		return true
	case method == http.MethodGet && strings.HasPrefix(path, "/assets/"):
		return true
	case (method == http.MethodPost && (path == "/api/session" || path == "/api/setup")) || (method == http.MethodDelete && path == "/api/session"):
		return true
	default:
		return false
	}
}

func registerSession(r gin.IRoutes, application *app.App, store *sessions) {
	r.GET("/api/session", func(c *gin.Context) {
		configured, err := application.OperatorConfigured(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		cookie, err := c.Cookie(sessionCookie)
		authenticated := err == nil && store.valid(c.Request.Context(), cookie)
		c.JSON(http.StatusOK, gin.H{"configured": configured, "authenticated": authenticated, "version": version.Version})
	})
	r.POST("/api/setup", func(c *gin.Context) {
		var body struct {
			Password string `json:"password"`
		}
		if !bindJSON(c, &body) {
			return
		}
		if err := application.SetOperatorPassword(c.Request.Context(), body.Password); err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"configured": true})
	})
	r.POST("/api/session", func(c *gin.Context) {
		var body struct {
			Password string `json:"password"`
		}
		if !bindJSON(c, &body) {
			return
		}
		if err := application.CheckOperatorPassword(c.Request.Context(), body.Password); err != nil {
			if errors.Is(err, app.ErrPassword) {
				application.Audit(c.Request.Context(), "session.refused", "wrong password from "+clientAddr(c.Request))
			}
			writeError(c, err)
			return
		}
		application.Audit(c.Request.Context(), "session.open", "signed in from "+clientAddr(c.Request))
		token, err := store.issue()
		if err != nil {
			writeError(c, err)
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(sessionCookie, token, 12*3600, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"authenticated": true})
	})
	r.DELETE("/api/session", func(c *gin.Context) {
		if cookie, err := c.Cookie(sessionCookie); err == nil {
			if store.valid(c.Request.Context(), cookie) {
				application.Audit(c.Request.Context(), "session.close", "signed out from "+clientAddr(c.Request))
			}
			store.drop(cookie)
		}
		c.SetCookie(sessionCookie, "", -1, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
	})
	r.PUT("/api/session/password", func(c *gin.Context) {
		var body struct {
			Current string `json:"current"`
			Password string `json:"password"`
		}
		if !bindJSON(c, &body) {
			return
		}
		if err := application.ChangeOperatorPassword(c.Request.Context(), body.Current, body.Password); err != nil {
			writeError(c, err)
			return
		}
		if cookie, err := c.Cookie(sessionCookie); err == nil {
			store.drop(cookie)
		}
		c.SetCookie(sessionCookie, "", -1, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
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
