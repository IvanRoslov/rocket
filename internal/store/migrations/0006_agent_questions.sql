-- Q&A-треды ролей (задача #639): полный аналог task_questions/question_messages,
-- только адресат — роль, а не задача. Направление треда выводится из авторства:
-- пустой asked_by/author = человек, иначе session id инстанса роли
-- ("<role>-run-<n>").
CREATE TABLE agent_questions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  role_id     TEXT NOT NULL REFERENCES agents(id),
  asked_by    TEXT NOT NULL DEFAULT '',      -- session id инстанса роли; '' = человек
  body        TEXT NOT NULL,
  context     TEXT,                          -- опциональный markdown-контекст
  status      TEXT NOT NULL DEFAULT 'open',  -- open|resolved
  resolution  TEXT,                          -- answered|dismissed (когда resolved)
  asked_at    INTEGER NOT NULL,
  resolved_at INTEGER
);

CREATE TABLE agent_question_messages (       -- тред вопроса роли
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES agent_questions(id),
  author      TEXT,                          -- session id инстанса роли; NULL = человек
  kind        TEXT NOT NULL DEFAULT 'reply', -- reply|answer
  body        TEXT NOT NULL,
  created_at  INTEGER NOT NULL
);

CREATE INDEX idx_agent_questions ON agent_questions(role_id, status);
CREATE INDEX idx_agent_question_messages ON agent_question_messages(question_id, id);
