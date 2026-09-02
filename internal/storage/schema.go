package storage

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS objects (
  hash TEXT PRIMARY KEY,
  size INTEGER NOT NULL,
  mime TEXT NOT NULL DEFAULT 'application/octet-stream',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS threads (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  baseline_commit TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  worktree_path TEXT NOT NULL DEFAULT '',
  read_only INTEGER NOT NULL DEFAULT 0,
  revision INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS git_snapshots (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  head TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  unborn INTEGER NOT NULL DEFAULT 0,
  status_object TEXT REFERENCES objects(hash),
  staged_patch_object TEXT REFERENCES objects(hash),
  unstaged_patch_object TEXT REFERENCES objects(hash),
  untracked_manifest_object TEXT REFERENCES objects(hash),
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS git_snapshots_thread_created
  ON git_snapshots(thread_id, created_at);

CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  closed_at INTEGER
);
CREATE INDEX IF NOT EXISTS agent_runs_thread_started
  ON agent_runs(thread_id, started_at);

CREATE TABLE IF NOT EXISTS rounds (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  round_number INTEGER NOT NULL,
  parent_round_id TEXT REFERENCES rounds(id),
  kind TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  snapshot_id TEXT REFERENCES git_snapshots(id),
  agent_run_id TEXT REFERENCES agent_runs(id),
  event_from_seq INTEGER NOT NULL DEFAULT 0,
  event_to_seq INTEGER NOT NULL DEFAULT 0,
  manifest_object TEXT REFERENCES objects(hash),
  created_at INTEGER NOT NULL,
  UNIQUE(thread_id, round_number)
);
CREATE INDEX IF NOT EXISTS rounds_thread_number
  ON rounds(thread_id, round_number);
CREATE TRIGGER IF NOT EXISTS rounds_immutable_update
BEFORE UPDATE ON rounds
BEGIN
  SELECT RAISE(ABORT, 'rounds are immutable');
END;
CREATE TRIGGER IF NOT EXISTS rounds_immutable_delete
BEFORE DELETE ON rounds
BEGIN
  SELECT RAISE(ABORT, 'rounds are immutable');
END;

CREATE TABLE IF NOT EXISTS evidence (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  round_id TEXT REFERENCES rounds(id),
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT '',
  object_hash TEXT REFERENCES objects(hash),
  metadata BLOB,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS evidence_thread_created
  ON evidence(thread_id, created_at);

CREATE TABLE IF NOT EXISTS shares (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  secret_hash TEXT NOT NULL,
  server_spki TEXT NOT NULL,
  capabilities BLOB NOT NULL,
  expires_at INTEGER NOT NULL,
  revoked_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS shares_thread_created
  ON shares(thread_id, created_at);

CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY,
  share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  joined_at INTEGER NOT NULL,
  last_seen INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participants_share_seen
  ON participants(share_id, last_seen);

CREATE TABLE IF NOT EXISTS annotations (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  round_id TEXT REFERENCES rounds(id),
  participant_id TEXT REFERENCES participants(id),
  path TEXT NOT NULL DEFAULT '',
  start_line INTEGER NOT NULL DEFAULT 0,
  end_line INTEGER NOT NULL DEFAULT 0,
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS annotations_thread_created
  ON annotations(thread_id, created_at);

CREATE TABLE IF NOT EXISTS control_leases (
  share_id TEXT PRIMARY KEY REFERENCES shares(id) ON DELETE CASCADE,
  participant_id TEXT REFERENCES participants(id),
  epoch INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
  share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
  command_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  expected_revision INTEGER NOT NULL,
  lease_epoch INTEGER NOT NULL,
  request_object TEXT REFERENCES objects(hash),
  status TEXT NOT NULL,
  result_object TEXT REFERENCES objects(hash),
  error_text TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  completed_at INTEGER,
  PRIMARY KEY(share_id, command_id)
);

CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload BLOB NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS events_thread_seq ON events(thread_id, seq);
`
