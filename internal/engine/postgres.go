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
	manifestReader, manifestWriter := io.Pipe()
	var files []ManifestFile
	var walkErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		files, walkErr = WalkBaseBackupTar(manifestReader, codec)
		_ = manifestReader.Close()
	}()

	stderr, err := runBaseBackup(ctx, src, codec, io.MultiWriter(output, manifestWriter))
	_ = manifestWriter.Close()
	<-done
	if err != nil {
		return Format{}, fmt.Errorf("pg_basebackup: %s", redact(err.Error(), src.Password))
	}
	if walkErr != nil {
		return Format{}, fmt.Errorf("walk base backup for manifest: %w", walkErr)
	}
	lsn := ParseBaseBackupLSN(stderr)
	format := Format{
		Name: "pg-base-" + codec, Extension: ".tar", CompressInPipeline: false,
		StartLSN: lsn.StartLSN, StopLSN: lsn.StopLSN, TimelineID: lsn.TimelineID,
	}
	if src.SystemID != 0 && lsn.StartLSN != "" && lsn.StopLSN != "" {
		manifest, buildErr := BuildBackupManifest(files, src.SystemID, lsn.TimelineID, lsn.StartLSN, lsn.StopLSN)
		if buildErr != nil {
			return Format{}, buildErr
		}
		format.Manifest = manifest
	}
	return format, nil
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

func runBaseBackup(ctx context.Context, src Source, codec string, output io.Writer) (string, error) {
	pass, err := writePGPass(src)
	if err != nil {
		return "", err
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
	if src.IncrementalManifest != "" {
		args = append(args, "--incremental="+src.IncrementalManifest)
	}
	env := []string{"PGPASSFILE=" + pass.path}
	if src.SSL {
		env = append(env, "PGSSLMODE=require")
	} else {
		env = append(env, "PGSSLMODE=disable")
	}
	return runCapture(ctx, "pg_basebackup", args, env, nil, output, 64<<10)
}

func restorePostgres(ctx context.Context, src Source, input io.Reader) error {
	pass, err := writePGPass(src)
	if err != nil {
		return err
	}
	defer pass.remove()
	// -L and -j both need a seekable archive. The body is staged only for this
	// restore, never on the backup upload path. The list drops extensions and
	// user mappings so a target that does not have them can still load the dump.
	dir, err := os.MkdirTemp("", "dmsx-pgrestore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	archive := filepath.Join(dir, "backup.dump")
	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, input); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	env := pgEnv(src, pass.path)
	var listed strings.Builder
	if _, err := runCapture(ctx, "pg_restore", []string{"-l", archive}, env, nil, &listed, 64<<10); err != nil {
		return err
	}
	toc := filepath.Join(dir, "toc.list")
	if err := os.WriteFile(toc, []byte(filterRestoreTOC(listed.String())), 0o600); err != nil {
		return err
	}
	return run(ctx, "pg_restore", postgresRestoreArgs(src, toc, archive), env, nil, io.Discard)
}

func pgEnv(src Source, passPath string) []string {
	mode := "disable"
	if src.SSL {
		mode = "require"
	}
	return []string{"PGPASSFILE=" + passPath, "PGSSLMODE=" + mode}
}

// postgresRestoreArgs leaves object ownership and grants to the restoring role.
// A single job wraps the load in one transaction. Parallel jobs cannot.
func postgresRestoreArgs(src Source, tocPath, archivePath string) []string {
	jobs := src.Parallel
	if jobs < 1 {
		jobs = 1
	}
	if jobs > 32 {
		jobs = 32
	}
	args := []string{
		"--no-password",
		"-h", src.Host,
		"-p", strconv.Itoa(src.Port),
		"-U", src.User,
		"-d", src.Database,
		"--no-owner",
		"--no-acl",
	}
	if jobs > 1 {
		args = append(args, "-j", strconv.Itoa(jobs))
	} else {
		args = append(args, "--single-transaction")
	}
	if tocPath != "" {
		args = append(args, "-L", tocPath)
	}
	if archivePath != "" {
		args = append(args, archivePath)
	}
	return args
}

// filterRestoreTOC drops extension and user-mapping entries from a pg_restore -l list.
func filterRestoreTOC(list string) string {
	var out strings.Builder
	for _, line := range strings.Split(list, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if !strings.HasPrefix(trimmed, ";") && (strings.Contains(upper, " EXTENSION ") || strings.Contains(upper, " USER MAPPING ")) {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

type pgpass struct{ path, dir string }

// PreparePGPass writes a mode-0600 password file and returns its path plus a
// cleanup that removes the temp directory.
func PreparePGPass(src Source) (path string, cleanup func(), err error) {
	p, err := writePGPass(src)
	if err != nil {
		return "", func() {}, err
	}
	return p.path, p.remove, nil
}

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
