package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func dumpMySQL(ctx context.Context, src Source, output io.Writer, maria bool) (Format, error) {
	bin := "mysqldump"
	product := "mysql"
	format := Format{Name: "mysql-sql", Extension: ".sql.zst", CompressInPipeline: true}
	if maria {
		bin = "mariadb-dump"
		product = "mariadb"
		format.Name = "mariadb-sql"
	}
	cnf, err := writeMyCNF(src, maria)
	if err != nil {
		return Format{}, err
	}
	defer cnf.remove()

	base := append([]string{cnf.arg()}, myClientArgs(ctx, bin, maria)...)
	var candidates [][]string
	if maria || strings.HasPrefix(src.Version, "5.7") {
		candidates = [][]string{{"--compress"}}
	} else {
		candidates = [][]string{
			{"--compression-algorithms=zstd", "--zstd-compression-level=5"},
			{"--compression-algorithms=zlib"},
		}
	}
	compression := probeCompression(ctx, bin, base, src.Database, candidates)
	args := append(base, compression...)
	args = append(args, mysqlDumpArgs(src, maria)...)
	if err := run(ctx, bin, args, nil, nil, output); err != nil {
		return Format{}, fmt.Errorf("%s dump: %s", product, redact(err.Error(), src.Password))
	}
	return format, nil
}

func mysqlDumpArgs(src Source, maria bool) []string {
	args := []string{
		"--single-transaction",
		"--routines",
		"--quick",
		"--skip-add-locks",
		"--no-tablespaces",
		"--max-allowed-packet=1G",
	}
	if !maria {
		args = append(args, "--set-gtid-purged=OFF")
	}
	if src.IncludeTriggers {
		args = append(args, "--triggers")
	} else {
		args = append(args, "--skip-triggers")
	}
	if src.IncludeEvents {
		args = append(args, "--events")
	}
	for _, table := range src.ExcludeTables {
		args = append(args, "--ignore-table="+src.Database+"."+table)
	}
	if maria {
		if src.SSL {
			args = append(args, "--ssl", "--skip-ssl-verify-server-cert")
		} else {
			args = append(args, "--skip-ssl")
		}
	} else if src.SSL {
		args = append(args, "--ssl-mode=REQUIRED")
	} else {
		args = append(args, "--ssl-mode=DISABLED")
	}
	return append(args, src.Database)
}

func restoreMySQL(ctx context.Context, src Source, input io.Reader, maria bool) error {
	bin := "mysql"
	if maria {
		bin = "mariadb"
	}
	cnf, err := writeMyCNF(src, maria)
	if err != nil {
		return err
	}
	defer cnf.remove()
	args := append([]string{cnf.arg()}, myClientArgs(ctx, bin, maria)...)
	args = append(args, src.Database)
	return run(ctx, bin, args, nil, input, io.Discard)
}

type mycnf struct{ dir string }

func writeMyCNF(src Source, maria bool) (*mycnf, error) {
	dir, err := os.MkdirTemp("", "dmsx-mycnf-*")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	ssl := "ssl-mode=DISABLED"
	if maria {
		ssl = "ssl=false"
		if src.SSL {
			ssl = "ssl=true"
		}
	} else if src.SSL {
		ssl = "ssl-mode=REQUIRED"
	}
	password := strings.ReplaceAll(src.Password, `\`, `\\`)
	password = strings.ReplaceAll(password, `"`, `\"`)
	body := fmt.Sprintf("[client]\nuser=%s\npassword=\"%s\"\nhost=%s\nport=%d\n%s\n",
		src.User, password, src.Host, src.Port, ssl)
	if err := os.WriteFile(filepath.Join(dir, ".my.cnf"), []byte(body), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &mycnf{dir: dir}, nil
}

func (c *mycnf) arg() string { return "--defaults-file=" + filepath.Join(c.dir, ".my.cnf") }

func (c *mycnf) remove() { os.RemoveAll(c.dir) }
