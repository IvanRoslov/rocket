-- Durable state for the role GitHub subscription poller (task #639, #643).
--
-- agent_gh_watermark holds the "we started watching this repo at" timestamp
-- per (role, repo): the first poll seeds it and enqueues nothing, so
-- subscribing a role to a busy repository does not dump the whole open-issue
-- backlog into its inbox. Later polls only consider issues/comments created
-- after the watermark, and use it as the `since` parameter of the GitHub
-- list calls.
--
-- agent_gh_seen is the durable dedup set: an issue number or comment id is
-- inserted at the moment its inbox event is enqueued, so a daemon restart can
-- never enqueue the same event twice. Issue numbers and comment ids live in
-- separate `kind` namespaces because they collide numerically.
CREATE TABLE agent_gh_watermark (
  role_id    TEXT NOT NULL REFERENCES agents(id),
  repo       TEXT NOT NULL,              -- owner/repo
  since      INTEGER NOT NULL,           -- unix seconds
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (role_id, repo)
);

CREATE TABLE agent_gh_seen (
  role_id     TEXT NOT NULL REFERENCES agents(id),
  repo        TEXT NOT NULL,             -- owner/repo
  kind        TEXT NOT NULL,             -- issue_opened|issue_comment
  external_id INTEGER NOT NULL,          -- issue number | comment id
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (role_id, repo, kind, external_id)
);

CREATE INDEX idx_agent_gh_seen_created ON agent_gh_seen(created_at);
