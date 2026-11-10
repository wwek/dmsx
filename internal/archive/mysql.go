package archive

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"dmsx/internal/sqlident"
)

// MySQLTable archives one table through primary-key batches.
type MySQLTable struct {
	db       *sql.DB
	database string
	table    string
	where    string
	pk       PrimaryKey
	cols     []Column
}

func OpenMySQL(ctx context.Context, user, password, host string, port int, database, table, where string) (*MySQLTable, error) {
	if err := sqlident.ValidatePredicate(where); err != nil {
		return nil, err
	}
	if !sqlident.Valid(table) {
		return nil, fmt.Errorf("invalid table name")
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
	t := &MySQLTable{db: db, database: database, table: table, where: where}
	if err := t.loadMeta(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return t, nil
}

func (t *MySQLTable) loadMeta(ctx context.Context) error {
	pk, err := discoverPrimaryKey(ctx, t.db, t.database, t.table)
	if err != nil {
		return err
	}
	t.pk = pk
	quoted, err := sqlident.QuoteMySQL(t.table)
	if err != nil {
		return err
	}
	rows, err := t.db.QueryContext(ctx, "SELECT * FROM "+quoted+" WHERE 1=0")
	if err != nil {
		return err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return err
	}
	t.cols = make([]Column, len(types))
	for i, column := range types {
		kind := KindOf(strings.ToUpper(column.DatabaseTypeName()))
		if strings.Contains(strings.ToUpper(column.DatabaseTypeName()), "UNSIGNED") {
			kind = KindString
		}
		t.cols[i] = Column{Name: column.Name(), Kind: kind}
	}
	return nil
}

func discoverPrimaryKey(ctx context.Context, db *sql.DB, database, table string) (PrimaryKey, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pk PrimaryKey
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		pk = append(pk, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := pk.validate(); err != nil {
		return nil, err
	}
	return pk, nil
}

func (t *MySQLTable) PrimaryKey(context.Context) (PrimaryKey, error) { return t.pk, nil }
func (t *MySQLTable) Columns(context.Context) ([]Column, error)       { return t.cols, nil }
func (t *MySQLTable) Close()                                          { _ = t.db.Close() }

func (t *MySQLTable) FetchChunk(ctx context.Context, after []any, limit int) ([][]any, error) {
	if limit <= 0 {
		limit = 5000
	}
	quoted, err := sqlident.QuoteMySQL(t.table)
	if err != nil {
		return nil, err
	}
	order, err := t.pk.orderBy()
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(after)+1)
	query := "SELECT * FROM " + quoted + " WHERE (" + t.where + ")"
	if len(after) > 0 {
		if len(after) != len(t.pk) {
			return nil, fmt.Errorf("after key length %d does not match primary key %d", len(after), len(t.pk))
		}
		clause, err := t.pk.afterClause()
		if err != nil {
			return nil, err
		}
		query += " AND " + clause
		args = append(args, after...)
	}
	query += " ORDER BY " + order + " LIMIT ?"
	args = append(args, limit)
	rows, err := t.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		values := make([]any, len(t.cols))
		ptrs := make([]any, len(t.cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, value := range values {
			if raw, ok := value.([]byte); ok {
				values[i] = append([]byte(nil), raw...)
			}
		}
		out = append(out, values)
	}
	return out, rows.Err()
}

func (t *MySQLTable) DeleteKeys(ctx context.Context, keys [][]any) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	quoted, err := sqlident.QuoteMySQL(t.table)
	if err != nil {
		return 0, err
	}
	cols, err := t.pk.quoted()
	if err != nil {
		return 0, err
	}
	var total int64
	const maxIN = 500
	for start := 0; start < len(keys); start += maxIN {
		end := start + maxIN
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)*len(t.pk))
		for i, key := range chunk {
			if len(key) != len(t.pk) {
				return total, fmt.Errorf("delete key length %d does not match primary key %d", len(key), len(t.pk))
			}
			inner := make([]string, len(t.pk))
			for j := range t.pk {
				inner[j] = "?"
				args = append(args, key[j])
			}
			placeholders[i] = "(" + strings.Join(inner, ", ") + ")"
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE (%s) IN (%s)", quoted, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
		result, err := t.db.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		total += n
	}
	return total, nil
}
