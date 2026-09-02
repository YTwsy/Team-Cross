package storage

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

// Version zero is the original v0 schema. Never rewrite immutable history.
func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema %d is newer than supported %d", version, schemaVersion)
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, `
ALTER TABLE annotations ADD COLUMN target BLOB;
CREATE TABLE session_snapshots (
 id TEXT PRIMARY KEY, thread_id TEXT NOT NULL REFERENCES threads(id),
 object_hash TEXT NOT NULL REFERENCES objects(hash), captured_at INTEGER NOT NULL
);
CREATE INDEX session_snapshots_thread ON session_snapshots(thread_id, captured_at, id);
CREATE TRIGGER session_snapshots_immutable_update BEFORE UPDATE ON session_snapshots
 BEGIN SELECT RAISE(ABORT, 'session snapshots are immutable'); END;
CREATE TRIGGER session_snapshots_immutable_delete BEFORE DELETE ON session_snapshots
 BEGIN SELECT RAISE(ABORT, 'session snapshots are immutable'); END;
CREATE TABLE share_projections (share_id TEXT PRIMARY KEY REFERENCES shares(id), payload BLOB NOT NULL);
CREATE TABLE run_bindings (run_id TEXT PRIMARY KEY REFERENCES agent_runs(id), payload BLOB NOT NULL);
PRAGMA user_version = 1;
`); err != nil {
			return fmt.Errorf("migrate review schema: %w", err)
		}
	}
	return tx.Commit()
}
