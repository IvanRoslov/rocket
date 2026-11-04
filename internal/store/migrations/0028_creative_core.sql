CREATE TABLE creative_pipelines (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 project_id TEXT NOT NULL REFERENCES projects(id),
 name TEXT NOT NULL,
 stages TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE creative_runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 project_id TEXT NOT NULL REFERENCES projects(id),
 pipeline_id INTEGER NOT NULL REFERENCES creative_pipelines(id),
 pipeline_name TEXT NOT NULL,
 name TEXT NOT NULL,
 brief TEXT NOT NULL,
 orchestrator_session_id TEXT REFERENCES sessions(id),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX creative_runs_project ON creative_runs(project_id,id);
CREATE TABLE creative_stages (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 position INTEGER NOT NULL,
 definition TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','in_progress','awaiting_approval','completed')),
 progress INTEGER NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
 progress_note TEXT NOT NULL DEFAULT '',
 review_revision INTEGER NOT NULL DEFAULT 0,
 artifact_version_ids TEXT NOT NULL DEFAULT '[]',
 UNIQUE(run_id,position)
);
CREATE TABLE creative_items (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id INTEGER NOT NULL REFERENCES creative_runs(id),
 stage_id INTEGER NOT NULL REFERENCES creative_stages(id),
 position INTEGER NOT NULL,
 name TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 expected_artifacts TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','in_progress','awaiting_approval','completed')),
 progress INTEGER NOT NULL DEFAULT 0 CHECK(progress BETWEEN 0 AND 100),
 progress_note TEXT NOT NULL DEFAULT '',
 review_revision INTEGER NOT NULL DEFAULT 0,
 artifact_version_ids TEXT NOT NULL DEFAULT '[]',
 UNIQUE(stage_id,position)
);
