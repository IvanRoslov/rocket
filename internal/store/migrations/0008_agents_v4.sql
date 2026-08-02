-- Постоянные агенты v4 (задача #639): managed-слой выпилен целиком. Агент —
-- это запись в реестре плюс фиксированное имя tmux-сессии; rocket только
-- регистрирует его, доставляет сообщения и хранит инбокс. Ни wake-движка, ни
-- досье, ни подписок на GitHub, ни файловой памяти больше нет.
--
-- Старые строки agent_inbox сознательно НЕ переносятся: событийный инбокс v2
-- (issue_opened/cron/snooze_expired/...) не имеет смысла в модели v4, а сама
-- фича ни разу не работала в проде — переносить нечего.
DROP TABLE IF EXISTS agent_gh_seen;
DROP TABLE IF EXISTS agent_gh_watermark;
DROP TABLE IF EXISTS agent_items;
DROP TABLE IF EXISTS agent_inbox;

CREATE TABLE agent_inbox (            -- сообщения, ждущие разбора агентом
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id   TEXT NOT NULL REFERENCES agents(id),
  from_id    TEXT NOT NULL DEFAULT '',   -- id сессии отправителя; '' — человек/UI
  body       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('unread','read')),
  created_at INTEGER NOT NULL,
  read_at    INTEGER
);

CREATE INDEX idx_agent_inbox_agent ON agent_inbox(agent_id, status, id);

-- agents пересобирается: project_id становится необязательным (только
-- группировка в UI), managed-колонки (prompt_path, subscriptions, cron, agent)
-- уступают место паре лончера (dir, command) и описанию для людей.
CREATE TABLE agents_new (
  id          TEXT PRIMARY KEY,           -- [a-z0-9-], оно же имя tmux-сессии
  description TEXT NOT NULL DEFAULT '',
  project_id  TEXT NOT NULL DEFAULT '',   -- '' — агент вне проекта
  dir         TEXT NOT NULL DEFAULT '',   -- рабочая директория для agent start
  command     TEXT NOT NULL DEFAULT '',   -- команда запуска; '' — интерактивный shell
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

INSERT INTO agents_new (id, description, project_id, dir, command, enabled, created_at, updated_at)
SELECT id, '', project_id, '', '', enabled, created_at, updated_at FROM agents;

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;

CREATE INDEX idx_agents_project ON agents(project_id);
