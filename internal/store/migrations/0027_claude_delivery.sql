CREATE TABLE claude_launches (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 generation TEXT NOT NULL UNIQUE,
 launch_json TEXT NOT NULL,
 identity_json TEXT,
 last_event_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE claude_deliveries (
 message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
 transport TEXT NOT NULL CHECK(transport='claude-socket-v1'),
 wire_id TEXT NOT NULL UNIQUE,
 snapshot_json TEXT NOT NULL,
 claude_uuid TEXT NOT NULL,
 journal_device INTEGER NOT NULL,
 journal_inode INTEGER NOT NULL,
 journal_path TEXT NOT NULL,
 boundary_offset INTEGER NOT NULL,
 envelope_hash TEXT NOT NULL,
 phase TEXT NOT NULL,
 deadline_ns INTEGER NOT NULL,
 proof_record TEXT,
 proof_offset INTEGER,
 proof_end INTEGER
);
CREATE INDEX claude_history_domain ON claude_deliveries(claude_uuid,journal_device,journal_inode,envelope_hash,boundary_offset);
CREATE INDEX claude_history_cross_file ON claude_deliveries(claude_uuid,envelope_hash,phase);
CREATE TABLE claude_consumed_records (
 target_key TEXT NOT NULL,
 device INTEGER NOT NULL,
 inode INTEGER NOT NULL,
 record_id TEXT NOT NULL,
 message_id INTEGER NOT NULL UNIQUE REFERENCES claude_deliveries(message_id) ON DELETE CASCADE,
 PRIMARY KEY(target_key,device,inode,record_id)
);
