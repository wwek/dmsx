package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func dumpPostgresLogical(ctx context.Context, src Source, output io.Writer) (Format, error) {
	pass, err := writePGPass(src)
	if err != nil {
		return Format{}, err
	}
	defer pass.remove()
	args := []string{
		"-Fc",
		"--no-password",
		"-h", src.Host,
		"-p", strconv.Itoa(src.Port),
		"-U", src.User,
		"-d", src.Database,
		"--verbose",
	}
	for _, schema := range src.IncludeSchemas {
		args = append(args, "-n", schema)
	}
	for _, table := range src.ExcludeTables {
		args = append(args, "--exclude-table="+table)
	}
	for _, table := range src.ExcludeTableData {
		args = append(args, "--exclude-table-data="+table)
	}
	args = append(args, postgresCompressArgs(src.Version)...)
	env := []string{"PGPASSFILE=" + pass.path}
	if src.SSL {
		env = append(env, "PGSSLMODE=require")
	} else {
		env = append(env, "PGSSLMODE=disable")
	}
	if err := run(ctx, "pg_dump", args, env, nil, output); err != nil {
		return Format{}, fmt.Errorf("pg_dump: %s", redact(err.Error(), src.Password))
	}
	return Format{Name: "pg-custom", Extension: ".dump", CompressInPipeline: false}, nil
}

func postgresCompressArgs(version string) []string {
	major := majorOf(version)
	if major >= 16 || major == 0 && strings.Contains(strings.ToLower(version), "zstd") {
		if major >= 16 {
			return []string{"--compress=zstd:5"}
		}
	}
	if major > 0 && major < 16 {
		return []string{"-Z", "5"}
	}
	// Unknown servers get gzip, which every supported pg_dump accepts.
	return []string{"-Z", "5"}
}

func majorOf(version string) int {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0
	}
	head, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	return n
}

func dumpPostgresPhysical(ctx context.Context, src Source, output io.Writer) (Format, error) {
	codec := physicalCodec(src.PhysicalCodec)
	if err := runBaseBackup(ctx, src, codec, output); err != nil {
		return Format{}, fmt.Errorf("pg_basebackup: %s", redact(err.Error(), src.Password))
	}
	return Format{Name: "pg-base-" + codec, Extension: ".tar", CompressInPipeline: false}, nil
}

func physicalCodec(name string) string {
	switch strings.ToLower(name) {
	case "gzip":
		return "server-gzip:6"
	case "none":
		return "none"
	default:
		return "server-zstd:5"
	}
}

func runBaseBackup(ctx context.Context, src Source, codec string, output io.Writer) error {
	pass, err := writePGPass(src)
	if err != nil {
		return err
	}
	defer pass.remove()
	args := []string{
		"--pgdata=-",
		"--format=tar",
		"--compress=" + codec,
		"--wal-method=fetch",
		"--checkpoint=fast",
		"--no-manifest",
		"--label=dmsx",
		"--no-password",
		"--verbose",
		"-h", src.Host,
		"-p", strconv.Itoa(src.Port),
		"-U", src.User,
	}
	env := []string{"PGPASSFILE=" + pass.path}
	if src.SSL {
		env = append(env, "PGSSLMODE=require")
	} else {
		env = append(env, "PGSSLMODE=disable")
	}
	return run(ctx, "pg_basebackup", args, env, nil, output)
}

func restorePostgres(ctx context.Context, src Source, input io.Reader) error {
	pass, err := writePGPass(src)
	if err != nil {
		return err
	}
	defer pass.remove()
	args := []string{
		"--no-password",
		"-h", src.Host,
		"-p", strconv.Itoa(src.Port),
		"-U", src.User,
		"-d", src.Database,
		"--single-transaction",
	}
	env := []string{"PGPASSFILE=" + pass.path, "PGSSLMODE=prefer"}
	return run(ctx, "pg_restore", args, env, input, io.Discard)
}

type pgpass struct{ path, dir string }

func writePGPass(src Source) (*pgpass, error) {
	dir, err := os.MkdirTemp("", "dmsx-pgpass-*")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	password := strings.ReplaceAll(src.Password, `\`, `\\`)
	password = strings.ReplaceAll(password, `:`, `\:`)
	line := fmt.Sprintf("%s:%d:%s:%s:%s\n", src.Host, src.Port, src.Database, src.User, password)
	path := filepath.Join(dir, "pgpass")
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &pgpass{path: path, dir: dir}, nil
}

func (p *pgpass) remove() { os.RemoveAll(p.dir) }
