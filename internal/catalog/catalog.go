// Package catalog stores dmsx's own records in SQLite. Backup payloads are not
// stored here; this database only remembers connections, schedules and outcomes.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Catalog struct{ db *sql.DB }

func Open(path string) (*Catalog, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS records (
		kind TEXT NOT NULL,
		id TEXT NOT NULL,
		body TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (kind, id)
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Catalog{db: db}, nil
}

func (c *Catalog) Close() error { return c.db.Close() }

func (c *Catalog) Put(ctx context.Context, kind, id string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO records(kind, id, body, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(kind, id) DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at`,
		kind, id, string(body), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (c *Catalog) Get(ctx context.Context, kind, id string, dest any) error {
	var body string
	err := c.db.QueryRowContext(ctx, `SELECT body FROM records WHERE kind = ? AND id = ?`, kind, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s %s not found", kind, id)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), dest)
}

func (c *Catalog) List(ctx context.Context, kind string, dest any) error {
	rows, err := c.db.QueryContext(ctx, `SELECT body FROM records WHERE kind = ? ORDER BY updated_at DESC`, kind)
	if err != nil {
		return err
	}
	defer rows.Close()
	var raw []json.RawMessage
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return err
		}
		raw = append(raw, json.RawMessage(body))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if string(encoded) == "null" {
		encoded = []byte("[]")
	}
	return json.Unmarshal(encoded, dest)
}

// Prune keeps the keep most recently written records of kind.
func (c *Catalog) Prune(ctx context.Context, kind string, keep int) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM records WHERE kind = ? AND id NOT IN (
		SELECT id FROM records WHERE kind = ? ORDER BY updated_at DESC LIMIT ?)`, kind, kind, keep)
	return err
}

func (c *Catalog) Delete(ctx context.Context, kind, id string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM records WHERE kind = ? AND id = ?`, kind, id)
	return err
}
