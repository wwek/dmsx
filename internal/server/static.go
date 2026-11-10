package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:ui
var uiFS embed.FS

func registerAssets(r *gin.Engine) {
	root, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	assets, err := fs.Sub(root, "assets")
	if err != nil {
		panic(err)
	}
	r.GET("/assets/*filepath", gin.WrapH(http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))))
	r.GET("/favicon.svg", func(c *gin.Context) {
		data, err := fs.ReadFile(root, "favicon.svg")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/svg+xml", data)
	})
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			c.Status(http.StatusNotFound)
			return
		}
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		data, err := fs.ReadFile(root, "index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
}
