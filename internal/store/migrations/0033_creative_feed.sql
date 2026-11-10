CREATE TABLE creative_feed_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 stage_id INTEGER REFERENCES creative_stages(id),
 item_id INTEGER REFERENCES creative_items(id),
 kind TEXT NOT NULL CHECK(kind IN ('message','proposal')),
 author TEXT NOT NULL CHECK(author IN ('human','orchestrator')),
 author_session_id TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL,
 artifact_version_ids TEXT NOT NULL DEFAULT '[]',
 created_at INTEGER NOT NULL,
 card_status TEXT NOT NULL DEFAULT '',
 decision TEXT NOT NULL DEFAULT '',
 decided_at INTEGER NOT NULL DEFAULT 0,
 replaces_entry_id INTEGER REFERENCES creative_feed_entries(id),
 replaced_by_entry_id INTEGER REFERENCES creative_feed_entries(id)
);
CREATE INDEX creative_feed_scope ON creative_feed_entries(run_id,stage_id,id);
CREATE TABLE creative_feed_reads (
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 stage_id INTEGER NOT NULL DEFAULT 0,
 through_entry_id INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(run_id,stage_id)
);
-- Durable human-event outbox: no live session is necessary at creation time.
-- message_id intentionally has no FK: transport retention must not erase history.
CREATE TABLE creative_feed_deliveries (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 entry_id INTEGER NOT NULL REFERENCES creative_feed_entries(id),
 body TEXT NOT NULL,
 message_id INTEGER
);
CREATE INDEX creative_feed_delivery_run ON creative_feed_deliveries(run_id);
