CREATE TABLE creative_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL REFERENCES creative_runs(id),
    stage_id INTEGER REFERENCES creative_stages(id),
    item_id INTEGER REFERENCES creative_items(id),
    name TEXT NOT NULL CHECK(length(trim(name)) > 0),
    kind TEXT NOT NULL CHECK(kind IN ('source','result')),
    current_version_id INTEGER REFERENCES creative_artifact_versions(id),
    created_at INTEGER NOT NULL,
    CHECK(item_id IS NULL OR stage_id IS NOT NULL)
);
CREATE UNIQUE INDEX creative_artifact_name ON creative_artifacts(run_id, COALESCE(stage_id,0), COALESCE(item_id,0), name, kind);
CREATE TABLE creative_artifact_versions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    artifact_id INTEGER NOT NULL REFERENCES creative_artifacts(id),
    version_number INTEGER NOT NULL CHECK(version_number > 0),
    filename TEXT NOT NULL,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL CHECK(size > 0),
    sha256 TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    author_session_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE(artifact_id,version_number)
);
