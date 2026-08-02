-- Единая схема вопросных тредов (задача #722). До этой миграции тред был
-- жёстким диалогом двух сторон и жил в одной из двух почти одинаковых пар
-- таблиц: task_questions/question_messages (тред задачи) и
-- agent_questions/agent_question_messages (тред роли постоянного агента).
-- Теперь тред один: questions с необязательными task_id и role_id плюс явный
-- список участников question_participants.
--
-- Переносим так, чтобы не потерять ни истории, ни порядковых номеров ("Q3"):
-- треды задач сохраняют свои id как есть, треды ролей приезжают со сдвигом на
-- максимальный id треда задачи. Сдвиг — константа, поэтому и порядок внутри
-- роли, и ссылки сообщений на треды остаются корректными.

-- 1. questions — это перестроенная task_questions: task_id становится
--    nullable, добавляется nullable role_id. Тред привязан к задаче, к роли,
--    либо ни к чему.
CREATE TABLE questions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id     INTEGER REFERENCES tasks(id),   -- NULL — тред не привязан к задаче
  role_id     TEXT REFERENCES agents(id),     -- NULL — тред не привязан к роли
  asked_by    TEXT NOT NULL DEFAULT '',       -- session id автора; '' = человек
  body        TEXT NOT NULL,
  context     TEXT,                           -- опциональный markdown-контекст
  status      TEXT NOT NULL DEFAULT 'open',   -- open|resolved
  resolution  TEXT,                           -- answered|dismissed (когда resolved)
  asked_at    INTEGER NOT NULL,
  resolved_at INTEGER
);

INSERT INTO questions (id, task_id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
SELECT id, task_id, NULL, asked_by, body, context, status, resolution, asked_at, resolved_at
FROM task_questions;

-- 2. Треды ролей — со сдвигом на максимальный id треда задачи. Подзапрос читает
--    ещё живую task_questions, поэтому сдвиг одинаков для всех строк.
INSERT INTO questions (id, task_id, role_id, asked_by, body, context, status, resolution, asked_at, resolved_at)
SELECT (SELECT COALESCE(MAX(id), 0) FROM task_questions) + aq.id,
       NULL, aq.role_id, aq.asked_by, aq.body, aq.context, aq.status, aq.resolution,
       aq.asked_at, aq.resolved_at
FROM agent_questions aq;

-- 3. question_messages перестраивается ради addressed_to — CSV идентификаторов
--    участников; пусто означает «всем участникам, кроме автора».
CREATE TABLE question_messages_new (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id  INTEGER NOT NULL REFERENCES questions(id),
  author       TEXT NOT NULL DEFAULT 'human', -- id участника; 'human' = человек
  kind         TEXT NOT NULL DEFAULT 'reply', -- reply|answer
  body         TEXT NOT NULL,
  addressed_to TEXT NOT NULL DEFAULT '',      -- CSV участников; '' = всем, кроме автора
  created_at   INTEGER NOT NULL
);

-- Автор нормализуется здесь же: раньше человек писался как NULL или '',
-- теперь у колонок с идентификаторами участников нет пустого варианта.
INSERT INTO question_messages_new (id, question_id, author, kind, body, addressed_to, created_at)
SELECT id, question_id, COALESCE(NULLIF(author, ''), 'human'), kind, body, '', created_at
FROM question_messages;

-- 4. Сообщения тредов ролей: свой сдвиг для id сообщения и тот же сдвиг тредов
--    для question_id. Оба подзапроса читают ещё не удалённые старые таблицы.
INSERT INTO question_messages_new (id, question_id, author, kind, body, addressed_to, created_at)
SELECT (SELECT COALESCE(MAX(id), 0) FROM question_messages) + m.id,
       (SELECT COALESCE(MAX(id), 0) FROM task_questions) + m.question_id,
       COALESCE(NULLIF(m.author, ''), 'human'), m.kind, m.body, '', m.created_at
FROM agent_question_messages m;

-- 5. Старые таблицы больше не нужны; их индексы удаляются вместе с ними.
DROP TABLE agent_question_messages;
DROP TABLE agent_questions;
DROP TABLE question_messages;
DROP TABLE task_questions;

ALTER TABLE question_messages_new RENAME TO question_messages;

-- 6. Участники треда. Участником становятся, когда тебя указали в --to или
--    когда ты сам написал в тред; человек — участник любого треда всегда.
CREATE TABLE question_participants (
  question_id    INTEGER NOT NULL REFERENCES questions(id),
  participant_id TEXT NOT NULL,             -- 'human' | id агента | session id
  added_at       INTEGER NOT NULL,
  UNIQUE (question_id, participant_id)
);

-- 7. Бэкфилл участников существующих тредов: человек, автор вопроса и все
--    авторы сообщений. Пустой asked_by — это человек, он уже добавлен первым
--    запросом.
INSERT OR IGNORE INTO question_participants (question_id, participant_id, added_at)
SELECT id, 'human', asked_at FROM questions;

INSERT OR IGNORE INTO question_participants (question_id, participant_id, added_at)
SELECT id, asked_by, asked_at FROM questions WHERE asked_by <> '';

INSERT OR IGNORE INTO question_participants (question_id, participant_id, added_at)
SELECT question_id, author, MIN(created_at) FROM question_messages
GROUP BY question_id, author;

CREATE INDEX idx_questions_task ON questions(task_id, status);
CREATE INDEX idx_questions_role ON questions(role_id, status);
CREATE INDEX idx_question_messages ON question_messages(question_id, id);
CREATE INDEX idx_question_participants_participant ON question_participants(participant_id, question_id);
