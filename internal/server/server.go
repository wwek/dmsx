package server

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"dmsx/internal/app"
	"dmsx/internal/healthcheck"
)

//go:embed assets/*
var assets embed.FS

func New(application *app.App) http.Handler {
	mux := http.NewServeMux()
	store := newSessions(application.PasswordChangedAt)
	registerSession(mux, application, store)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/databases", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListDatabases(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/databases", func(w http.ResponseWriter, r *http.Request) {
		var in app.Database
		if !decode(w, r, &in) {
			return
		}
		row, err := application.CreateDatabase(r.Context(), in)
		respond(w, row, err)
	})
	mux.HandleFunc("DELETE /api/databases/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.DeleteDatabase(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/databases/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.TestDatabase(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/databases/{id}/backup-user", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Replication bool `json:"replication"`
		}
		if !decode(w, r, &in) {
			return
		}
		row, err := application.CreateBackupUser(r.Context(), r.PathValue("id"), in.Replication)
		respond(w, row, err)
	})
	mux.HandleFunc("PUT /api/databases/{id}/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		var in healthcheck.Config
		if !decode(w, r, &in) {
			return
		}
		row, err := application.SetHealthCheck(r.Context(), r.PathValue("id"), in)
		respond(w, row, err)
	})
	mux.HandleFunc("GET /api/healthchecks", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListHealth(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("GET /api/notifiers", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListNotifiers(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/notifiers", func(w http.ResponseWriter, r *http.Request) {
		var in app.Notifier
		if !decode(w, r, &in) {
			return
		}
		row, err := application.CreateNotifier(r.Context(), in)
		respond(w, row, err)
	})
	mux.HandleFunc("DELETE /api/notifiers/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.DeleteNotifier(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/notifiers/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.TestNotifier(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /api/audit", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListAudit(r.Context(), 500)
		respond(w, rows, err)
	})
	mux.HandleFunc("GET /api/storages", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListStorages(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/storages", func(w http.ResponseWriter, r *http.Request) {
		var in app.Storage
		if !decode(w, r, &in) {
			return
		}
		row, err := application.CreateStorage(r.Context(), in)
		respond(w, row, err)
	})
	mux.HandleFunc("DELETE /api/storages/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.DeleteStorage(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/storages/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.TestStorage(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /api/backups/configs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListConfigs(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/backups/configs", func(w http.ResponseWriter, r *http.Request) {
		var in app.BackupConfig
		if !decode(w, r, &in) {
			return
		}
		row, err := application.CreateConfig(r.Context(), in)
		respond(w, row, err)
	})
	mux.HandleFunc("DELETE /api/backups/configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.DeleteConfig(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/backups/configs/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		row, err := application.StartBackup(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, row)
	})
	mux.HandleFunc("GET /api/backups/runs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListRuns(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		application.Cancel(r.PathValue("id"))
		application.Audit(r.Context(), "job.cancel", r.PathValue("id"))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("DELETE /api/backups/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]bool{"ok": true}, application.DeleteRun(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /api/backups/restores", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListRestores(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/backups/runs/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		var in app.RestoreTarget
		if !decode(w, r, &in) {
			return
		}
		row, err := application.StartRestore(r.Context(), r.PathValue("id"), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, row)
	})
	mux.HandleFunc("GET /api/backups/runs/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		body, name, err := application.OpenRun(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+name)
		_, _ = io.Copy(w, body)
	})
	mux.HandleFunc("GET /api/archive/jobs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListArchives(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("POST /api/archive/jobs", func(w http.ResponseWriter, r *http.Request) {
		var in app.ArchiveJob
		if !decode(w, r, &in) {
			return
		}
		row, err := application.StartArchive(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, row)
	})
	mux.HandleFunc("POST /api/cold-query", func(w http.ResponseWriter, r *http.Request) {
		var in app.ColdRequest
		if !decode(w, r, &in) {
			return
		}
		rows, err := application.ColdQuery(r.Context(), in)
		respond(w, map[string]any{"rows": rows}, err)
	})
	mux.HandleFunc("GET /api/ddl/runs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := application.ListDDL(r.Context())
		respond(w, rows, err)
	})
	mux.HandleFunc("GET /api/ddl/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		row, err := application.GetDDL(r.Context(), r.PathValue("id"))
		respond(w, row, err)
	})
	mux.HandleFunc("POST /api/ddl/runs", func(w http.ResponseWriter, r *http.Request) {
		var in app.DDLRun
		if !decode(w, r, &in) {
			return
		}
		row, err := application.StartDDL(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, row)
	})
	mux.HandleFunc("GET /api/ddl/runs/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := conn.CloseRead(r.Context())
		events, unsubscribe := application.DDLEvents(r.PathValue("id"))
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case payload, ok := <-events:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, payload)
				cancel()
				if err != nil {
					return
				}
			}
		}
	})
	mux.HandleFunc("GET /", serveAsset("assets/index.html", "text/html; charset=utf-8"))
	mux.HandleFunc("GET /favicon.svg", serveAsset("assets/favicon.svg", "image/svg+xml"))
	mux.HandleFunc("GET /app.css", serveAsset("assets/app.css", "text/css; charset=utf-8"))
	mux.HandleFunc("GET /app.js", serveAsset("assets/app.js", "text/javascript; charset=utf-8"))
	return store.guard(mux)
}

func serveAsset(name, contentType string) http.HandlerFunc {
	body, err := assets.ReadFile(name)
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func respond(w http.ResponseWriter, body any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if code := authStatus(err); code != 0 {
		status = code
	} else if strings.Contains(err.Error(), "not found") {
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
