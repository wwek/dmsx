package engine

import (
	"context"
	"fmt"
	"strings"
)

// QueryMySQL runs one SQL statement through the mysql/mariadb client.
func QueryMySQL(ctx context.Context, src Source, sql string) (string, error) {
	bin := "mysql"
	maria := src.Engine == MariaDB
	if maria {
		bin = "mariadb"
	}
	cnf, err := writeMyCNF(src, maria)
	if err != nil {
		return "", err
	}
	defer cnf.remove()
	args := []string{cnf.arg(), "-N", "-B", "-e", sql, src.Database}
	var stdout strings.Builder
	if err := run(ctx, bin, args, nil, nil, &stdout); err != nil {
		return "", fmt.Errorf("%s: %s", bin, redact(err.Error(), src.Password))
	}
	return stdout.String(), nil
}
