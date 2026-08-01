-- Persistent agent roles (task #639): registry, event inbox and dossier.
-- The sessions table is rebuilt because its kind CHECK constraint has to
-- accept the new 'agent' value (role instances). Migrations run with
-- foreign_keys=OFF on a dedicated connection, so dropping sessions while
-- tasks.session_id references it is safe; the self-reference below is written
-- against the final table name so it stays correct after the rename.
CREATE TABLE sessions_new (
  id            TEXT PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN ('orchestrator','worker','agent')),
  project_id    TEXT NOT NULL,
  repo_id       TEXT NOT NULL,
  feature_slug  TEXT NOT NULL,
  parent_id     TEXT REFERENCES sessions(id),
  agent         TEXT NOT NULL,
  branch        TEXT NOT NULL,
  worktree_path TEXT NOT NULL,
  tmux_name     TEXT NOT NULL,
  state         TEXT NOT NULL,
  activity      TEXT,
  activity_ts   INTEGER,
  pr_number     INTEGER,
  pr_state      TEXT,
  ci_state      TEXT,
  prompt        TEXT,
  pending_quiz  TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

INSERT INTO sessions_new (
  id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
  worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
  ci_state, prompt, pending_quiz, created_at, updated_at
)
SELECT
  id, kind, project_id, repo_id, feature_slug, parent_id, agent, branch,
  worktree_path, tmux_name, state, activity, activity_ts, pr_number, pr_state,
  ci_state, prompt, pending_quiz, created_at, updated_at
FROM sessions;

DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE INDEX idx_sessions_state ON sessions(state);
CREATE INDEX idx_sessions_feature ON sessions(feature_slug);

CREATE TABLE agents (                          -- роль (постоянный агент)
  id            TEXT PRIMARY KEY,              -- "sre", [a-z0-9-]
  project_id    TEXT NOT NULL REFERENCES projects(id),
  prompt_path   TEXT NOT NULL,                 -- <home>/agents/<id>/role.md
  subscriptions TEXT NOT NULL DEFAULT '[]',    -- JSON [{repo,labels[],mention_only}]
  cron          TEXT NOT NULL DEFAULT '',
  agent         TEXT NOT NULL DEFAULT '',      -- underlying agent (claude-code|codex)
  enabled       INTEGER NOT NULL DEFAULT 1,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE agent_inbox (                     -- очередь событий роли
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id    TEXT NOT NULL REFERENCES agents(id),
  kind       TEXT NOT NULL,                    -- message|issue_opened|issue_comment|
                                               -- task_update|snooze_expired|cron|
                                               -- question|terminal_opened
  payload    TEXT NOT NULL DEFAULT '{}',       -- JSON
  status     TEXT NOT NULL DEFAULT 'queued',   -- queued|delivered|done
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE agent_items (                     -- досье роли
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id      TEXT NOT NULL REFERENCES agents(id),
  kind         TEXT NOT NULL,                  -- issue|task|ping
  external_ref TEXT NOT NULL,                  -- owner/repo#123 | task:45 | msg:<id>
  state        TEXT NOT NULL DEFAULT 'new',    -- new|triaged|taken|deferred|
                                               -- waiting_team|in_work|resolved|closed
  note         TEXT NOT NULL DEFAULT '',
  task_id      INTEGER,
  snooze_until INTEGER,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE (role_id, kind, external_ref)
);

CREATE INDEX idx_agents_project ON agents(project_id);
CREATE INDEX idx_agent_inbox_role ON agent_inbox(role_id, status, id);
CREATE INDEX idx_agent_items_role ON agent_items(role_id, state);
