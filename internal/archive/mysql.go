package archive

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"dmsx/internal/sqlident"
)

// MySQLCursor reads with Rows.Next so the driver keeps one batch in flight
// instead of buffering the whole result.
type MySQLCursor struct {
	db   *sql.DB
	rows *sql.Rows
	cols []Column
}

func OpenMySQL(ctx context.Context, user, password, host string, port int, database, table, where string) (*MySQLCursor, error) {
	quotedDB, err := sqlident.QuoteMySQL(database)
	if err != nil {
		return nil, err
	}
	_ = quotedDB
	quotedTable, err := sqlident.QuoteMySQL(table)
	if err != nil {
		return nil, err
	}
	if err := sqlident.ValidatePredicate(where); err != nil {
		return nil, err
	}
	cfg := mysqldriver.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, fmt.Sprint(port))
	cfg.DBName = database
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	rows, err := db.QueryContext(ctx, "SELECT * FROM "+quotedTable+" WHERE "+where)
	if err != nil {
		db.Close()
		return nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		db.Close()
		return nil, err
	}
	cols := make([]Column, len(types))
	for i, column := range types {
		kind := KindOf(strings.ToUpper(column.DatabaseTypeName()))
		if strings.Contains(strings.ToUpper(column.DatabaseTypeName()), "UNSIGNED") {
			kind = KindString
		}
		cols[i] = Column{Name: column.Name(), Kind: kind}
	}
	return &MySQLCursor{db: db, rows: rows, cols: cols}, nil
}

func (c *MySQLCursor) Columns(context.Context) ([]Column, error) { return c.cols, nil }

func (c *MySQLCursor) Next(context.Context) ([]any, error) {
	if !c.rows.Next() {
		if err := c.rows.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	values := make([]any, len(c.cols))
	ptrs := make([]any, len(c.cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := c.rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	for i, value := range values {
		if raw, ok := value.([]byte); ok {
			values[i] = append([]byte(nil), raw...)
		}
	}
	return values, nil
}

func (c *MySQLCursor) Close() {
	c.rows.Close()
	c.db.Close()
}

// MySQLDeleter issues DELETE ... LIMIT in batches. The predicate is the same
// one the cursor archived, and it runs only after the caller checked the file.
type MySQLDeleter struct {
	User     string
	Password string
	Host     string
	Port     int
	Database string
	Table    string
	Where    string
}

func (d MySQLDeleter) DeleteBatches(ctx context.Context, batch int, pause func()) (int64, error) {
	quoted, err := sqlident.QuoteMySQL(d.Table)
	if err != nil {
		return 0, err
	}
	if err := sqlident.ValidatePredicate(d.Where); err != nil {
		return 0, err
	}
	cfg := mysqldriver.NewConfig()
	cfg.User = d.User
	cfg.Passwd = d.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(d.Host, fmt.Sprint(d.Port))
	cfg.DBName = d.Database
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	query := fmt.Sprintf("DELETE FROM %s WHERE (%s) LIMIT %d", quoted, d.Where, batch)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		result, err := db.ExecContext(ctx, query)
		if err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		total += n
		if n == 0 {
			return total, nil
		}
		if pause != nil {
			pause()
		}
	}
}
