package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PostgresSystemID reads pg_control_system().system_identifier through psql.
func PostgresSystemID(ctx context.Context, src Source) (uint64, error) {
	out, err := QueryPostgres(ctx, src, "SELECT system_identifier FROM pg_control_system()")
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse system identifier %q: %w", out, err)
	}
	return id, nil
}

// QueryPostgres runs one SQL statement with psql -tAc and returns stdout.
func QueryPostgres(ctx context.Context, src Source, sql string) (string, error) {
	return runPSQL(ctx, src, sql)
}

func runPSQL(ctx context.Context, src Source, sql string) (string, error) {
	pass, err := writePGPass(src)
	if err != nil {
		return "", err
	}
	defer pass.remove()
	args := []string{
		"--no-password",
		"-h", src.Host,
		"-p", strconv.Itoa(src.Port),
		"-U", src.User,
		"-d", src.Database,
		"-tAc", sql,
	}
	env := []string{"PGPASSFILE=" + pass.path}
	if src.SSL {
		env = append(env, "PGSSLMODE=require")
	} else {
		env = append(env, "PGSSLMODE=disable")
	}
	var stdout strings.Builder
	if err := run(ctx, "psql", args, env, nil, &stdout); err != nil {
		return "", fmt.Errorf("psql: %s", redact(err.Error(), src.Password))
	}
	return stdout.String(), nil
}
