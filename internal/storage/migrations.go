package storage

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 3

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
		version = 1
	}
	if version == 1 {
		if _, err := tx.ExecContext(ctx, `
CREATE TABLE session_follows (
 id TEXT PRIMARY KEY, thread_id TEXT NOT NULL REFERENCES threads(id),
 provider TEXT NOT NULL, session_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('active','retrying','stopped')),
 epoch INTEGER NOT NULL, cursor TEXT NOT NULL, current_snapshot_id TEXT NOT NULL REFERENCES session_snapshots(id),
 payload BLOB NOT NULL
);
CREATE UNIQUE INDEX session_follows_active ON session_follows(thread_id,provider,session_id) WHERE state != 'stopped';
PRAGMA user_version = 2;
`); err != nil {
			return fmt.Errorf("migrate Follow schema: %w", err)
		}
		version = 2
	}
	if version == 2 {
		if _, err := tx.ExecContext(ctx, `
CREATE TABLE share_native_live (
 share_id TEXT PRIMARY KEY REFERENCES shares(id),
 follow_id TEXT NOT NULL REFERENCES session_follows(id),
 follow_epoch INTEGER NOT NULL CHECK(follow_epoch > 0),
 binding BLOB NOT NULL,
 start_seq INTEGER NOT NULL CHECK(start_seq >= 0),
 state TEXT NOT NULL CHECK(state IN ('active','limited')),
 latest_snapshot_id TEXT NOT NULL REFERENCES session_snapshots(id),
 window_count INTEGER NOT NULL CHECK(window_count >= 0 AND window_count <= 128),
 projection_bytes INTEGER NOT NULL CHECK(projection_bytes >= 0 AND projection_bytes <= 67108864)
);
CREATE INDEX share_native_live_follow ON share_native_live(follow_id,follow_epoch,state);
CREATE TRIGGER share_native_live_binding_immutable BEFORE UPDATE OF share_id,follow_id,follow_epoch,binding,start_seq ON share_native_live
 BEGIN SELECT RAISE(ABORT, 'native live Share bindings are immutable'); END;
CREATE TABLE share_live_snapshots (
 share_id TEXT NOT NULL REFERENCES share_native_live(share_id),
 snapshot_id TEXT NOT NULL REFERENCES session_snapshots(id),
 projection_object TEXT NOT NULL REFERENCES objects(hash),
 fully_shared INTEGER NOT NULL CHECK(fully_shared IN (0,1)),
 ordinal INTEGER NOT NULL CHECK(ordinal > 0 AND ordinal <= 128),
 projection_bytes INTEGER NOT NULL CHECK(projection_bytes >= 0),
 admitted_at INTEGER NOT NULL,
 PRIMARY KEY(share_id,snapshot_id), UNIQUE(share_id,ordinal)
);
CREATE TRIGGER share_live_snapshots_immutable_update BEFORE UPDATE ON share_live_snapshots
 BEGIN SELECT RAISE(ABORT, 'native live Share windows are immutable'); END;
CREATE TRIGGER share_live_snapshots_immutable_delete BEFORE DELETE ON share_live_snapshots
 BEGIN SELECT RAISE(ABORT, 'native live Share windows are immutable'); END;
PRAGMA user_version = 3;
`); err != nil {
			return fmt.Errorf("migrate native live Share schema: %w", err)
		}
	}
	return tx.Commit()
}
