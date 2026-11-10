package engine

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// ServerPort reports the port the server says it listens on. It differs from
// src.Port whenever the connection is translated on the way in, by a port
// mapping, a proxy or a tunnel, and the difference is what gh-ost reports as
// an unexpected port.
func ServerPort(ctx context.Context, src Source) (int, error) {
	if src.Engine != MySQL && src.Engine != MariaDB {
		return 0, fmt.Errorf("only MySQL and MariaDB report a server port")
	}
	cfg := mysqldriver.NewConfig()
	cfg.User = src.User
	cfg.Passwd = src.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(src.Host, strconv.Itoa(src.Port))
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var port int
	if err := db.QueryRowContext(ctx, "SELECT @@global.port").Scan(&port); err != nil {
		return 0, err
	}
	return port, nil
}
