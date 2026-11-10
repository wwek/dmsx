package engine

import (
	"context"
	"fmt"
	"io"
	"strconv"
)

// Ping checks that the client tool can reach the server. It does not dump data.
func Ping(ctx context.Context, src Source) error {
	switch src.Engine {
	case MySQL, MariaDB:
		bin := "mysql"
		maria := src.Engine == MariaDB
		if maria {
			bin = "mariadb"
		}
		cnf, err := writeMyCNF(src, maria)
		if err != nil {
			return err
		}
		defer cnf.remove()
		args := append([]string{cnf.arg()}, myClientArgs(ctx, bin, maria)...)
		args = append(args, "-e", "SELECT 1")
		return run(ctx, bin, args, nil, nil, io.Discard)
	case PostgreSQL:
		pass, err := writePGPass(src)
		if err != nil {
			return err
		}
		defer pass.remove()
		args := []string{"--no-password", "-h", src.Host, "-p", strconv.Itoa(src.Port), "-U", src.User, "-d", src.Database, "-c", "SELECT 1"}
		return run(ctx, "psql", args, []string{"PGPASSFILE=" + pass.path}, nil, io.Discard)
	case MongoDB:
		return PingMongo(ctx, src)
	default:
		return fmt.Errorf("unknown engine %q", src.Engine)
	}
}
