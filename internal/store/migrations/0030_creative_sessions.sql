CREATE TABLE creative_session_contexts (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id),
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 stage_id INTEGER REFERENCES creative_stages(id),
 item_id INTEGER REFERENCES creative_items(id)
);
CREATE INDEX creative_sessions_run ON creative_session_contexts(run_id);
CREATE TABLE creative_discussion_contexts (
 message_id INTEGER UNIQUE REFERENCES messages(id),
 question_id INTEGER UNIQUE REFERENCES questions(id),
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 stage_id INTEGER REFERENCES creative_stages(id),
 item_id INTEGER REFERENCES creative_items(id),
 artifact_version_ids TEXT NOT NULL DEFAULT '[]',
 CHECK ((message_id IS NULL) != (question_id IS NULL))
);
CREATE INDEX creative_discussion_scope ON creative_discussion_contexts(run_id,stage_id,item_id);
