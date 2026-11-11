// Package engine runs the native dump and restore tools. Their stdout is the
// backup stream. Nothing here writes that stream to a local file.
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Source struct {
	Engine              string
	Host                string
	Port                int
	User                string
	Password            string
	Database            string
	SSL                 bool
	Version             string
	IncludeTriggers     bool
	IncludeEvents       bool
	ExcludeTables       []string
	IncludeSchemas      []string
	ExcludeTableData    []string
	Physical            bool
	PhysicalCodec       string
	IncrementalManifest string // local path to parent backup_manifest for PG 17+
	SystemID            uint64
	Parallel            int
}

type Format struct {
	Name               string
	Extension          string
	CompressInPipeline bool
	StartLSN           string
	StopLSN            string
	TimelineID         int
	Manifest           []byte
}

const (
	MySQL      = "mysql"
	MariaDB    = "mariadb"
	PostgreSQL = "postgresql"
	MongoDB    = "mongodb"
)

func Dump(ctx context.Context, src Source, output io.Writer) (Format, error) {
	switch src.Engine {
	case MySQL:
		return dumpMySQL(ctx, src, output, false)
	case MariaDB:
		return dumpMySQL(ctx, src, output, true)
	case PostgreSQL:
		if src.Physical {
			return dumpPostgresPhysical(ctx, src, output)
		}
		return dumpPostgresLogical(ctx, src, output)
	case MongoDB:
		return dumpMongo(ctx, src, output)
	default:
		return Format{}, fmt.Errorf("unknown engine %q", src.Engine)
	}
}

func Restore(ctx context.Context, src Source, format Format, input io.Reader) error {
	switch format.Name {
	case "mysql-sql":
		return restoreMySQL(ctx, src, input, false)
	case "mariadb-sql":
		return restoreMySQL(ctx, src, input, true)
	case "pg-custom":
		return restorePostgres(ctx, src, input)
	case "mongo-archive":
		return restoreMongo(ctx, src, input)
	default:
		return fmt.Errorf("format %q is restored by downloading the stream, not by piping it back", format.Name)
	}
}

func run(ctx context.Context, bin string, args []string, env []string, stdin io.Reader, stdout io.Writer) error {
	_, err := runCapture(ctx, bin, args, env, stdin, stdout, 16<<10)
	return err
}

func runCapture(ctx context.Context, bin string, args []string, env []string, stdin io.Reader, stdout io.Writer, stderrMax int) (string, error) {
	path, err := look(bin)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr tail
	stderr.max = stderrMax
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("%s: %w: %s", bin, err, strings.TrimSpace(stderr.String()))
	}
	return stderr.String(), nil
}

func look(name string) (string, error) {
	env := "DMSX_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if value := os.Getenv(env); value != "" {
		return value, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s was not found; set %s or install the client tools", name, env)
	}
	return path, nil
}

type tail struct {
	max int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }

func redact(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}

// probeCompression runs a schema-only dump and keeps the first candidate the
// server accepts. A rejected algorithm is not a failed backup.
func probeCompression(ctx context.Context, bin string, base []string, database string, candidates [][]string) []string {
	for _, candidate := range candidates {
		args := append([]string{}, base...)
		args = append(args, candidate...)
		args = append(args, "--no-data", database)
		err := run(ctx, bin, args, nil, nil, io.Discard)
		if err == nil {
			return candidate
		}
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "unknown option") || strings.Contains(text, "unknown variable") || strings.Contains(text, "compression algorithm") {
			continue
		}
		return nil
	}
	return nil
}
