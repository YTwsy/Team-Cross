package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"teamcross/internal/domain"
)

const DefaultBusyTimeout = 5 * time.Second

type Store struct {
	db      *sql.DB
	objects *ObjectStore
}

// Open creates or opens a Team Cross data directory. The SQLite database and
// content-addressed objects live beneath dataDir.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	objects, err := NewObjectStore(filepath.Join(dataDir, "objects", "sha256"))
	if err != nil {
		return nil, err
	}
	databaseURL := &url.URL{Scheme: "file", Path: filepath.Join(dataDir, "teamcross.db")}
	dsn := databaseURL.String() + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize sqlite schema: %w", err)
	}
	return &Store{db: db, objects: objects}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func fromMillis(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullMillis(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return millis(v)
}
