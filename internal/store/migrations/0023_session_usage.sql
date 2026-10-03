ALTER TABLE sessions ADD COLUMN task_id INTEGER;
ALTER TABLE sessions ADD COLUMN subtask_id INTEGER;

CREATE TABLE session_usage (
  session_id  TEXT NOT NULL,
  model       TEXT NOT NULL,
  input       INTEGER NOT NULL DEFAULT 0,
  cache_write INTEGER NOT NULL DEFAULT 0,
  cache_read  INTEGER NOT NULL DEFAULT 0,
  output      INTEGER NOT NULL DEFAULT 0,
  reasoning   INTEGER NOT NULL DEFAULT 0,
  messages    INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, model)
);

CREATE TABLE session_stats (
  session_id   TEXT PRIMARY KEY,
  task_id      INTEGER,
  subtask_id   INTEGER,
  status       TEXT NOT NULL,
  final        INTEGER NOT NULL,
  started_at   INTEGER NOT NULL,
  ended_at     INTEGER,
  collected_at INTEGER NOT NULL,
  error        TEXT NOT NULL DEFAULT '',
  attempts     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX session_stats_ended ON session_stats(ended_at);
CREATE INDEX session_stats_task ON session_stats(task_id);

CREATE TABLE model_prices (
  model       TEXT PRIMARY KEY,
  input       REAL,
  cache_write REAL,
  cache_read  REAL,
  output      REAL,
  updated_at  INTEGER NOT NULL
);
