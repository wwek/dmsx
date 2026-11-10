package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"dmsx/internal/app"
	"dmsx/internal/healthcheck"
)

// New builds the Gin engine that serves the console and the JSON API.
// The shape follows Databasus (Apache-2.0): gin.New, recovery, route groups.
// The handlers and session rules are dmsx's own. The console is a Vite/React
// build embedded from ui/.
func New(application *app.App) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	store := newSessions(application.PasswordChangedAt)
	r.Use(store.requireAuth())

	registerSession(r, application, store)
	registerAPI(r, application)
	registerAssets(r)
	return r
}

func registerAPI(r gin.IRoutes, application *app.App) {
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/api/databases", func(c *gin.Context) {
		rows, err := application.ListDatabases(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/databases", func(c *gin.Context) {
		var in app.Database
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateDatabase(c.Request.Context(), in)
		respond(c, row, err)
	})
	r.PUT("/api/databases/:id", func(c *gin.Context) {
		var in app.Database
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.UpdateDatabase(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.DELETE("/api/databases/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteDatabase(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/databases/:id/test", func(c *gin.Context) {
		respondOK(c, application.TestDatabase(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/databases/:id/backup-user", func(c *gin.Context) {
		var in struct {
			Replication bool `json:"replication"`
		}
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateBackupUser(c.Request.Context(), c.Param("id"), in.Replication)
		respond(c, row, err)
	})
	r.PUT("/api/databases/:id/healthcheck", func(c *gin.Context) {
		var in healthcheck.Config
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.SetHealthCheck(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.GET("/api/healthchecks", func(c *gin.Context) {
		rows, err := application.ListHealth(c.Request.Context())
		respond(c, rows, err)
	})

	r.GET("/api/notifiers", func(c *gin.Context) {
		rows, err := application.ListNotifiers(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/notifiers/probe", func(c *gin.Context) {
		var in app.Notifier
		if !bindJSON(c, &in) {
			return
		}
		respondOK(c, application.ProbeNotifier(c.Request.Context(), in))
	})
	r.POST("/api/notifiers", func(c *gin.Context) {
		var in app.Notifier
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateNotifier(c.Request.Context(), in)
		respond(c, row, err)
	})
	r.PUT("/api/notifiers/:id", func(c *gin.Context) {
		var in app.Notifier
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.UpdateNotifier(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.DELETE("/api/notifiers/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteNotifier(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/notifiers/:id/test", func(c *gin.Context) {
		respondOK(c, application.TestNotifier(c.Request.Context(), c.Param("id")))
	})
	r.GET("/api/audit", func(c *gin.Context) {
		rows, err := application.ListAudit(c.Request.Context(), 500)
		respond(c, rows, err)
	})

	r.GET("/api/storages", func(c *gin.Context) {
		rows, err := application.ListStorages(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/storages/probe", func(c *gin.Context) {
		var in app.Storage
		if !bindJSON(c, &in) {
			return
		}
		respondOK(c, application.ProbeStorage(c.Request.Context(), in))
	})
	r.POST("/api/storages", func(c *gin.Context) {
		var in app.Storage
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateStorage(c.Request.Context(), in)
		respond(c, row, err)
	})
	r.PUT("/api/storages/:id", func(c *gin.Context) {
		var in app.Storage
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.UpdateStorage(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.DELETE("/api/storages/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteStorage(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/storages/:id/test", func(c *gin.Context) {
		respondOK(c, application.TestStorage(c.Request.Context(), c.Param("id")))
	})

	r.GET("/api/backups/configs", func(c *gin.Context) {
		rows, err := application.ListConfigs(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/backups/configs", func(c *gin.Context) {
		var in app.BackupConfig
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateConfig(c.Request.Context(), in)
		respond(c, row, err)
	})
	r.PUT("/api/backups/configs/:id", func(c *gin.Context) {
		var in app.BackupConfig
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.UpdateConfig(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.DELETE("/api/backups/configs/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteConfig(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/backups/configs/:id/run", func(c *gin.Context) {
		var in struct {
			ForceFull bool `json:"forceFull"`
		}
		_ = c.ShouldBindJSON(&in) // empty body means a normal run
		row, err := application.StartBackup(c.Request.Context(), c.Param("id"), in.ForceFull)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, row)
	})
	r.GET("/api/backups/runs", func(c *gin.Context) {
		rows, err := application.ListRuns(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/runs/:id/cancel", func(c *gin.Context) {
		application.Cancel(c.Param("id"))
		application.Audit(c.Request.Context(), "job.cancel", c.Param("id"))
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.DELETE("/api/backups/runs/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteRun(c.Request.Context(), c.Param("id")))
	})
	r.GET("/api/backups/restores", func(c *gin.Context) {
		rows, err := application.ListRestores(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/backups/runs/:id/restore", func(c *gin.Context) {
		var in app.RestoreTarget
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.StartRestore(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, row)
	})
	r.GET("/api/backups/runs/:id/download", func(c *gin.Context) {
		body, name, err := application.OpenRun(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		defer body.Close()
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = io.Copy(c.Writer, body)
	})
	r.GET("/api/wal/segments", func(c *gin.Context) {
		rows, err := application.ListWalSegments(c.Request.Context(), c.Query("databaseId"))
		respond(c, rows, err)
	})
	r.GET("/api/wal/pitr-guide", func(c *gin.Context) {
		text, err := application.PITRGuide(c.Request.Context(), c.Query("databaseId"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusOK, text)
	})

	r.GET("/api/archive/plans", func(c *gin.Context) {
		rows, err := application.ListArchivePlans(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/archive/plans", func(c *gin.Context) {
		var in app.ArchivePlan
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.CreateArchivePlan(c.Request.Context(), in)
		respond(c, row, err)
	})
	r.PUT("/api/archive/plans/:id", func(c *gin.Context) {
		var in app.ArchivePlan
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.UpdateArchivePlan(c.Request.Context(), c.Param("id"), in)
		respond(c, row, err)
	})
	r.DELETE("/api/archive/plans/:id", func(c *gin.Context) {
		respondOK(c, application.DeleteArchivePlan(c.Request.Context(), c.Param("id")))
	})
	r.POST("/api/archive/plans/:id/run", func(c *gin.Context) {
		row, err := application.RunArchivePlan(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, row)
	})
	r.GET("/api/archive/jobs", func(c *gin.Context) {
		rows, err := application.ListArchives(c.Request.Context())
		respond(c, rows, err)
	})
	r.POST("/api/archive/jobs", func(c *gin.Context) {
		var in app.ArchiveJob
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.StartArchive(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, row)
	})
	r.POST("/api/cold-query", func(c *gin.Context) {
		var in app.ColdRequest
		if !bindJSON(c, &in) {
			return
		}
		rows, err := application.ColdQuery(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"rows": rows})
	})

	r.GET("/api/ddl/runs", func(c *gin.Context) {
		rows, err := application.ListDDL(c.Request.Context())
		respond(c, rows, err)
	})
	r.GET("/api/ddl/runs/:id", func(c *gin.Context) {
		row, err := application.GetDDL(c.Request.Context(), c.Param("id"))
		respond(c, row, err)
	})
	r.POST("/api/ddl/runs", func(c *gin.Context) {
		var in app.DDLRun
		if !bindJSON(c, &in) {
			return
		}
		row, err := application.StartDDL(c.Request.Context(), in)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, row)
	})
	r.GET("/api/ddl/runs/:id/stream", func(c *gin.Context) {
		conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := conn.CloseRead(c.Request.Context())
		events, unsubscribe := application.DDLEvents(c.Param("id"))
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
}

func bindJSON(c *gin.Context, dest any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	if err := c.ShouldBindJSON(dest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func respond(c *gin.Context, body any, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, body)
}

func respondOK(c *gin.Context, err error) {
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func writeError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	if code := authStatus(err); code != 0 {
		status = code
	} else if strings.Contains(err.Error(), "not found") {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"error": err.Error()})
}
