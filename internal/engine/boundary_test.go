package engine

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The layers under the app must not reach up into it. internal/engine runs
// the native tools and stays free of the app and the server, so that the
// engine can be used without the request layer.
func TestLowerLayersDoNotImportTheApp(t *testing.T) {
	root := moduleRoot(t)
	forbidden := []string{"dmsx/internal/app", "dmsx/internal/server"}
	layers := []string{
		"archive", "backup", "coldquery", "engine", "healthcheck", "notify",
		"onlineddl", "retention", "schedule", "secret", "storage", "streamcrypt", "wal",
	}
	for _, layer := range layers {
		directory := filepath.Join(root, "internal", layer)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("%s: %v", layer, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, spec := range file.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				for _, bad := range forbidden {
					if imported == bad || strings.HasPrefix(imported, bad+"/") {
						t.Fatalf("%s imports %s", path, imported)
					}
				}
			}
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("the module root was not found")
		}
		directory = parent
	}
}
