// Package coldquery runs a SELECT against archived parquet through an in-memory
// DuckDB. For Qiniu and other S3 stores, httpfs reads only the row groups the
// filter needs. The parquet path is built from checked identifiers; the user's
// text is only the WHERE predicate.
package coldquery

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/marcboeker/go-duckdb/v2"

	"dmsx/internal/archive"
	"dmsx/internal/sqlident"
)

type Request struct {
	Database string
	Table    string
	Where    string
	Limit    int
	// LocalGlob is a filesystem glob. When it is set, S3 is not contacted.
	LocalGlob string
	Bucket    string
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	PathStyle bool
	UseSSL    bool
	SkipTLS   bool
	Prefix    string
}

type Row map[string]any

func Query(ctx context.Context, req Request) ([]Row, error) {
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 100
	}
	var predicate sqlident.Predicate
	if strings.TrimSpace(req.Where) != "" {
		parsed, err := sqlident.ParsePredicate(req.Where)
		if err != nil {
			return nil, err
		}
		predicate = parsed
	}
	glob, err := sourceGlob(req)
	if err != nil {
		return nil, err
	}
	connector, err := duckdb.NewConnector("", nil)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	if req.LocalGlob == "" {
		if err := configureS3(ctx, db, req); err != nil {
			return nil, err
		}
	}
	statement := "SELECT * FROM read_parquet(" + sqlString(glob) + ")"
	if !predicate.Empty() {
		statement += " WHERE " + predicate.ANSI()
	}
	statement += fmt.Sprintf(" LIMIT %d", req.Limit)
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []Row
	for rows.Next() {
		values := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := Row{}
		for i, name := range names {
			row[name] = normalize(values[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func sourceGlob(req Request) (string, error) {
	if req.LocalGlob != "" {
		return req.LocalGlob, nil
	}
	if req.Bucket == "" {
		return "", fmt.Errorf("cold query needs an S3 or Qiniu bucket, or a local archive directory")
	}
	suffix, err := archive.Glob(req.Database, req.Table)
	if err != nil {
		return "", err
	}
	return "s3://" + req.Bucket + "/" + archive.PrefixedKey(req.Prefix, suffix), nil
}

func configureS3(ctx context.Context, db *sql.DB, req Request) error {
	if _, err := db.ExecContext(ctx, "INSTALL httpfs"); err != nil {
		return fmt.Errorf("duckdb httpfs install: %w", err)
	}
	if _, err := db.ExecContext(ctx, "LOAD httpfs"); err != nil {
		return fmt.Errorf("duckdb httpfs load: %w", err)
	}
	for _, statement := range s3Statements(req) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func s3Statements(req Request) []string {
	endpoint := strings.TrimPrefix(strings.TrimPrefix(req.Endpoint, "https://"), "http://")
	endpoint = strings.TrimRight(endpoint, "/")
	region := req.Region
	if region == "" {
		region = "us-east-1"
	}
	style := "vhost"
	if req.PathStyle {
		style = "path"
	}
	ssl := "true"
	if !req.UseSSL {
		ssl = "false"
	}
	settings := []string{
		"SET s3_region=" + sqlString(region),
		"SET s3_endpoint=" + sqlString(endpoint),
		"SET s3_access_key_id=" + sqlString(req.AccessKey),
		"SET s3_secret_access_key=" + sqlString(req.SecretKey),
		"SET s3_url_style=" + sqlString(style),
		"SET s3_use_ssl=" + ssl,
	}
	if req.SkipTLS {
		settings = append(settings, "SET enable_server_cert_verification=false")
	}
	return settings
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func normalize(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}
